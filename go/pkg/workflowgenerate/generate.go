package workflowgenerate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
	"github.com/halbritt/striatum/go/pkg/workflowtemplates"
)

const (
	GeneratorSchemaVersion   = "striatum.workflow_generator.v1"
	PlanSchemaVersion        = "striatum.workflow_plan.v1"
	WorkflowSchemaVersion    = "striatum.workflow.v1"
	WorkflowSchemaVersionV11 = "striatum.workflow.v1.1"
)

var (
	shapes = set(
		"minimal", "review", "code_change", "human_checkpoint",
		"evidence_backed", "implementation_panel", "multi_review_synthesis",
		"multi_phase", "custom", "conversation",
		"falsification_gate", "cross_examination",
		"adjudicated_constraint_extraction", "divergent_ideation",
		"fog_of_war_review", "synaptic_prune", "verification_gate",
	)
	laneSets      = set("local", "single_agent", "author_reviewer", "multi_review", "custom")
	laneModifiers = set("supervised", "worktree_isolated", "constrained", "harness_profiled")
	optionKeys    = set(
		"review_postures", "max_revision_cycles", "include_support_ledger",
		"constraints", "required_enforcement", "harness_profiles",
		"reviewer_count", "role_pack", "role_packs", "adversary_pack",
		"adversary_packs", "proposal_count", "score_dimensions",
		"custom_job_artifacts", "supervision_compatible", "phases",
		"topic", "turns", "max_dialog_rounds", "falsifier_count", "include_scribe",
		"branch_count", "ideas_per_branch", "deepen_count", "frame_pack",
		"frame_packs", "score_weights", "problem_shape", "convergence_lane_id",
		"reconstructor_count", "participant_count",
		"gate_floor", "checks",
	)
	blockKinds = set(
		"draft", "review", "synthesis", "implementation", "test",
		"human_checkpoint", "support_ledger", "evidence_audit", "final_review",
		"conversation",
	)
	allowedPostures = set(
		"neutral", "devils_advocate", "security", "threat_model",
		"latency_performance", "ergonomics_dx", "accessibility",
		"compliance_license", "supply_chain",
	)
	constraintValues = map[string]map[string]struct{}{
		"network":             set("allowed", "disabled", "loopback_only"),
		"transcript_capture":  set("allowed", "disabled"),
		"repository_scope":    set("full", "write_scope_only"),
		"filesystem_writes":   set("allowed", "write_scope_only", "disabled"),
		"credential_exposure": set("allowed", "redacted", "disabled"),
	}
	enforcementLevels = set("not_enforced", "advisory", "best_effort", "enforced")
)

type Error struct {
	Message   string
	FieldPath string
	Hint      string
	Ref       string
}

func (e *Error) Error() string {
	return e.Message
}

type Spec struct {
	SchemaVersion string
	Shape         string
	LaneSet       string
	WorkflowID    string
	Name          string
	WorkflowVer   string
	Branch        map[string]any
	ScaffoldRoot  string
	ArtifactRoot  string
	Lanes         map[string]map[string]any
	Options       map[string]any
	LaneModifiers []string
	Plan          map[string]any
	ContextDocs   []any
	Parallelism   map[string]any
}

type Generated struct {
	Workflow   map[string]any
	Files      []map[string]any
	Metadata   map[string]any
	Warnings   []string
	Validation map[string]any
	Lint       map[string]any
}

func (g Generated) Map() map[string]any {
	return map[string]any{
		"workflow":   g.Workflow,
		"files":      g.Files,
		"metadata":   g.Metadata,
		"warnings":   g.Warnings,
		"validation": g.Validation,
		"lint":       g.Lint,
	}
}

func DefaultSpec(scaffoldRoot, artifactRoot, shape, laneSet string, lanes map[string]map[string]any, options map[string]any) (Spec, error) {
	safeSlug := path.Base(strings.TrimSuffix(scaffoldRoot, "/"))
	if safeSlug == "." || safeSlug == "/" || safeSlug == "" {
		safeSlug = "starter-workflow"
	}
	raw := map[string]any{
		"schema_version":   GeneratorSchemaVersion,
		"shape":            shape,
		"lane_set":         laneSet,
		"lane_modifiers":   []any{},
		"workflow_id":      safeSlug + "-starter",
		"name":             fmt.Sprintf("%s starter (%s)", safeSlug, strings.ReplaceAll(shape, "_", "-")),
		"workflow_version": time.Now().UTC().Format("2006-01-02"),
		"branch": map[string]any{
			"mode":           "confirm",
			"suggested_name": "striatum/" + safeSlug,
			"allow_dirty":    false,
		},
		"scaffold_root": scaffoldRoot,
		"artifact_root": artifactRoot,
		"lanes":         lanes,
		"options":       options,
		"context_docs":  []any{},
	}
	return SpecFromMap(raw)
}

func SpecFromMap(raw map[string]any) (Spec, error) {
	allowed := set("schema_version", "shape", "lane_set", "workflow_id", "name", "workflow_version", "branch", "scaffold_root", "artifact_root", "lanes", "options", "lane_modifiers", "plan", "context_docs", "parallelism")
	for key := range raw {
		if _, ok := allowed[key]; !ok {
			return Spec{}, genErr("unknown generator spec field: "+key, "spec."+key)
		}
	}
	schema, err := requiredString(raw, "schema_version", "spec")
	if err != nil {
		return Spec{}, err
	}
	if schema != GeneratorSchemaVersion {
		return Spec{}, genErr("unsupported generator schema_version", "spec.schema_version")
	}
	shape, err := choice(raw, "shape", shapes, "spec")
	if err != nil {
		// #111: when the requested shape is a catalog template that is
		// example-only (advertised for discovery but not generated), point the
		// operator at its example fixture instead of the generic "must be one of"
		// list — the catalog and the generator must otherwise agree (reconcile test).
		if rawShape, _ := raw["shape"].(string); rawShape != "" {
			if hint := exampleOnlyShapeHint(rawShape); hint != "" {
				return Spec{}, genErr(hint, "spec.shape")
			}
		}
		return Spec{}, err
	}
	laneSet, err := choice(raw, "lane_set", laneSets, "spec")
	if err != nil {
		return Spec{}, err
	}
	workflowID, err := requiredString(raw, "workflow_id", "spec")
	if err != nil {
		return Spec{}, err
	}
	name, err := requiredString(raw, "name", "spec")
	if err != nil {
		return Spec{}, err
	}
	version, err := requiredString(raw, "workflow_version", "spec")
	if err != nil {
		return Spec{}, err
	}
	branch, err := object(raw["branch"], "spec.branch")
	if err != nil {
		return Spec{}, err
	}
	scaffold, err := SafeScaffoldRoot(mustString(raw["scaffold_root"]), "spec.scaffold_root")
	if err != nil {
		return Spec{}, err
	}
	artifact, err := SafeRelativePath(mustString(raw["artifact_root"]), "spec.artifact_root")
	if err != nil {
		return Spec{}, err
	}
	lanes, err := lanesFrom(raw["lanes"], laneSet)
	if err != nil {
		return Spec{}, err
	}
	options, err := object(defaultAny(raw["options"], map[string]any{}), "spec.options")
	if err != nil {
		return Spec{}, err
	}
	for key := range options {
		if _, ok := optionKeys[key]; !ok {
			return Spec{}, genErr("unknown generator option: "+key, "spec.options."+key)
		}
	}
	modifiers, err := stringList(defaultAny(raw["lane_modifiers"], []any{}), "spec.lane_modifiers")
	if err != nil {
		return Spec{}, err
	}
	for idx, modifier := range modifiers {
		if _, ok := laneModifiers[modifier]; !ok {
			return Spec{}, genErr("unknown lane modifier", fmt.Sprintf("spec.lane_modifiers[%d]", idx))
		}
	}
	contextDocs := []any{}
	if value, ok := raw["context_docs"]; ok {
		list, ok := value.([]any)
		if !ok {
			return Spec{}, genErr("value must be a list of objects", "spec.context_docs")
		}
		for idx, item := range list {
			if _, ok := item.(map[string]any); !ok {
				return Spec{}, genErr("value must be a list of objects", fmt.Sprintf("spec.context_docs[%d]", idx))
			}
		}
		contextDocs = append(contextDocs, list...)
	}
	var parallelism map[string]any
	if value, ok := raw["parallelism"]; ok {
		parallelism, err = object(value, "spec.parallelism")
		if err != nil {
			return Spec{}, err
		}
	}
	var plan map[string]any
	if value, ok := raw["plan"]; ok {
		plan, err = object(value, "spec.plan")
		if err != nil {
			return Spec{}, err
		}
	}
	if shape == "custom" && plan == nil {
		return Spec{}, genErr("custom shape requires a plan", "spec.plan")
	}
	if shape != "custom" && plan != nil {
		return Spec{}, genErr("plan is valid only for custom shape", "spec.plan")
	}
	return Spec{
		SchemaVersion: schema, Shape: shape, LaneSet: laneSet, WorkflowID: workflowID,
		Name: name, WorkflowVer: version, Branch: branch, ScaffoldRoot: scaffold,
		ArtifactRoot: artifact, Lanes: lanes, Options: options, LaneModifiers: modifiers,
		Plan: plan, ContextDocs: contextDocs, Parallelism: parallelism,
	}, nil
}

// ApplyGenerateOption routes a single `--option key=value` pair from the
// `workflow generate` CLI into either the generator options map or the lanes
// spec map (#187). Lane-spec keys are shaped `lanes.<laneID>.<field>` — the
// vocabulary a lane set advertises in its required_options (e.g.
// `lanes.author.command`). They route into spec.lanes so the catalog's
// recommended lane sets are generatable from the CLI instead of forcing a
// hand-edit. For the argv-array lane fields (`command`, `capabilities`) the
// value is parsed as a JSON array; any other lane field, and any JSON-decodable
// value, is decoded best-effort, otherwise the raw string is kept. Every other
// key flows to the options map unchanged, where SpecFromMap's allowlist
// validates it.
func ApplyGenerateOption(options map[string]any, lanes map[string]any, key, value string) error {
	if laneID, field, ok := parseLaneOptionKey(key); ok {
		laneBody, _ := lanes[laneID].(map[string]any)
		if laneBody == nil {
			laneBody = map[string]any{}
			lanes[laneID] = laneBody
		}
		laneBody[field] = decodeLaneOptionValue(value)
		return nil
	}
	options[key] = value
	return nil
}

// parseLaneOptionKey splits a `lanes.<laneID>.<field>` option key into its lane
// id and field. It reports ok=false for any key that is not a lane-spec key, so
// the caller routes it to the options allowlist. The bare `lanes` key (the
// custom lane set's composite required_option) is not a per-lane field and is
// left to the options/spec path.
func parseLaneOptionKey(key string) (laneID string, field string, ok bool) {
	rest, found := strings.CutPrefix(key, "lanes.")
	if !found {
		return "", "", false
	}
	laneID, field, found = strings.Cut(rest, ".")
	if !found || laneID == "" || field == "" {
		return "", "", false
	}
	return laneID, field, true
}

// decodeLaneOptionValue decodes a lane-field value. The argv-array fields
// (command, capabilities) require a JSON array — the generator's stringList
// validator rejects a bare string with a clear field-scoped error if the
// operator forgets the brackets. Other fields decode any JSON scalar, falling
// back to the raw string.
func decodeLaneOptionValue(value string) any {
	var decoded any
	if err := json.Unmarshal([]byte(value), &decoded); err == nil {
		return decoded
	}
	return value
}

func Generate(spec Spec) (Generated, error) {
	warnings := []string{}
	if err := validateModifierMatrix(spec, &warnings); err != nil {
		return Generated{}, err
	}
	lanes, err := compileLanes(spec)
	if err != nil {
		return Generated{}, err
	}
	var jobs []map[string]any
	var edges []map[string]any
	var cycles []map[string]any
	var phases []map[string]any
	if spec.Shape == "custom" {
		jobs, edges, cycles, err = compileCustom(spec, lanes)
	} else {
		jobs, edges, cycles, phases, err = compileShape(spec)
	}
	if err != nil {
		return Generated{}, err
	}
	if jobs == nil {
		jobs = []map[string]any{}
	}
	if edges == nil {
		edges = []map[string]any{}
	}
	if cycles == nil {
		cycles = []map[string]any{}
	}
	if phases == nil {
		phases = []map[string]any{}
	}
	// #301: reconcile lane worktree isolation against the jobs that were actually
	// compiled. compileLanes applies the #242 per-job isolation default using a
	// lane-name heuristic (every non-`*reviewer*` lane is repo-write), but in
	// shapes like divergent_ideation the lane ring round-robins repo-write
	// diverge/deepen jobs onto every lane — including a lane named `reviewer`.
	// That lane then carries an autonomous repo-write job but no
	// worktree_isolation, so `workflow validate` / `run prepare` reject the
	// generator's own output. Deriving the repo-write lane set from the job graph
	// (the same source of truth the validator uses) keeps generate and validate
	// from disagreeing.
	applyRepoWriteWorktreeIsolation(lanes, jobs)
	if spec.Shape == "implementation_panel" {
		warnings = append(warnings, "implementation_panel generates a high-artifact workflow; review proposal_count, score_dimensions, and lane costs before running.")
	}
	if spec.Shape == "divergent_ideation" {
		warnings = append(warnings, "divergent_ideation fans out branch_count + deepen_count + 2 jobs, each a model invocation (~10 by default); review branch_count and lane costs before running.")
		if distinctModelFamilies(lanes, divergentLaneRing(spec)) < 2 {
			warnings = append(warnings, "divergent_ideation branches all run on a single model family; the cross-model convergence signal is degraded — use a multi-model lane set (e.g. a custom claude/codex/agy set) for genuine cross-family agreement.")
		}
	}
	roleIDs := map[string]struct{}{}
	for _, job := range jobs {
		roleIDs[fmt.Sprint(job["role_id"])] = struct{}{}
	}
	roles := rolesFor(sortedKeys(roleIDs))
	parallelism := spec.Parallelism
	if parallelism == nil {
		parallelism = defaultParallelism(spec)
	}
	schemaVersion := WorkflowSchemaVersion
	if isPhasedShape(spec.Shape) {
		schemaVersion = WorkflowSchemaVersionV11
	}
	workflow := map[string]any{
		"schema_version":   schemaVersion,
		"workflow_id":      spec.WorkflowID,
		"workflow_version": spec.WorkflowVer,
		"name":             spec.Name,
		"branch":           cloneMap(spec.Branch),
		"coordinator":      coordinator(spec, lanes),
		"lanes":            lanes,
		"roles":            roles,
		"context_docs":     append([]any{}, spec.ContextDocs...),
		"parallelism":      parallelism,
		"jobs":             jobs,
		"edges":            edges,
		"cycles":           cycles,
	}
	if isPhasedShape(spec.Shape) {
		workflow["phases"] = phases
	}
	if hasModifier(spec, "constrained") {
		workflow["operator_mode"] = "constrained"
	}
	// RFC 0093 / RFC 0064: a collaboration shape on the single-lane `local`
	// fixture set runs the adjudicator on the same lane as the holder/proposer it
	// adjudicates, so the same_model_adjudicator_pair lint (now CLI-refused)
	// would otherwise reject the generated starter workflow. A local fixture lane
	// is inherently same-model and is exactly the documented legitimate override
	// case, so record the inline acceptance — matching the cycle.allow_same_model
	// the local collaboration cycle already sets.
	if isCollaborationShape(spec.Shape) && spec.LaneSet == "local" {
		workflow["allow_same_model_review_pairing"] = true
	}
	// #288: the single_agent lane set runs one lane that both authors and reviews,
	// so any review/revision pairing it produces is structurally same-model and
	// unavoidable (there is no second lane to route review to). Record the inline
	// acceptance — matching the local-collaboration case above — so the generated
	// single_agent code_change scaffold validates out of the box.
	if spec.LaneSet == "single_agent" {
		workflow["allow_same_model_review_pairing"] = true
	}
	// RFC 0141: stamp the verification_gate's allowlist_status (FILLED vs
	// TEMPLATE_UNFILLED) so a downstream validator can tell a runnable builtin-only
	// gate from one that still needs per-host pins. Done before ValidateWorkflow so
	// the field is part of the validated, rendered workflow.json.
	if spec.Shape == "verification_gate" {
		if err := applyVerificationGateWorkflowFields(spec, workflow); err != nil {
			return Generated{}, err
		}
	}
	if hasModifier(spec, "harness_profiled") {
		profiles, err := harnessProfiles(spec)
		if err != nil {
			return Generated{}, err
		}
		workflow["harness_profiles"] = profiles
	}
	if err := ValidateWorkflow(workflow); err != nil {
		return Generated{}, err
	}
	graph := graphData(jobs, edges, cycles)
	files, err := renderFiles(spec, workflow, roles)
	if err != nil {
		return Generated{}, err
	}
	// RFC 0141: the verification_gate shape also emits the hashless intent template
	// and a .gitignore for the per-host pins, beyond the workflow.json + role/prompt
	// stubs renderFiles produces. Append them as shape-specific extras.
	if spec.Shape == "verification_gate" {
		extras, err := verificationGateExtras(spec)
		if err != nil {
			return Generated{}, err
		}
		files = append(files, extras...)
	}
	metadata := map[string]any{
		"shape":             spec.Shape,
		"lane_set":          spec.LaneSet,
		"lane_modifiers":    append([]string(nil), spec.LaneModifiers...),
		"graph":             graph,
		"catalog_templates": []string{spec.Shape, spec.LaneSet},
		"scaffold_root":     spec.ScaffoldRoot,
		"workflow_path":     spec.ScaffoldRoot + "/workflow.json",
	}
	if spec.Shape == "implementation_panel" {
		rolePacks, err := panelRolePacks(spec)
		if err != nil {
			return Generated{}, err
		}
		adversaryPacks, err := panelAdversaryPacks(spec)
		if err != nil {
			return Generated{}, err
		}
		proposalCount, err := panelProposalCount(spec)
		if err != nil {
			return Generated{}, err
		}
		scoreDimensions, err := panelScoreDimensions(spec)
		if err != nil {
			return Generated{}, err
		}
		metadata["role_packs"] = rolePacks
		metadata["adversary_packs"] = adversaryPacks
		metadata["proposal_count"] = proposalCount
		metadata["score_dimensions"] = scoreDimensions
	}
	if isCollaborationShape(spec.Shape) {
		metadata["shape_family"] = "collaboration"
		metadata["collaboration_shape_pack"] = "substance_gate_v1"
		metadata["topic"] = collaborationTopic(spec)
	}
	if spec.Shape == "divergent_ideation" {
		branchCount, _ := divergentBranchCount(spec)
		deepenCount, _ := divergentDeepenCount(spec, branchCount)
		problemShape, _ := divergentProblemShape(spec)
		convergenceLane, _ := divergentConvergenceLane(spec)
		frameIDs := []string{}
		for _, frame := range selectFrames(spec.WorkflowID, branchCount, problemShape) {
			frameIDs = append(frameIDs, frame.ID)
		}
		metadata["branch_count"] = branchCount
		metadata["deepen_count"] = deepenCount
		metadata["frames"] = frameIDs
		metadata["convergence_lane"] = convergenceLane
		metadata["problem_shape"] = problemShape
		metadata["model_families"] = distinctModelFamilies(lanes, divergentLaneRing(spec))
	}
	return Generated{
		Workflow:   workflow,
		Files:      files,
		Metadata:   metadata,
		Warnings:   warnings,
		Validation: map[string]any{"ok": true, "workflow_id": spec.WorkflowID},
		Lint:       lintWorkflow(workflow),
	}, nil
}

func GenerateFromMap(raw map[string]any) (Generated, error) {
	spec, err := SpecFromMap(raw)
	if err != nil {
		return Generated{}, err
	}
	return Generate(spec)
}

func compileLanes(spec Spec) (map[string]any, error) {
	if spec.LaneSet == "local" {
		lanes := map[string]any{
			"local": map[string]any{
				"adapter":       "process",
				"display_model": "Local Fixture",
				"command":       []string{"sh", "-c", "cat >/dev/null"},
				"capabilities":  []string{"write", "review"},
			},
		}
		return applyLaneModifiers(spec, lanes, set("local"))
	}
	ids := laneIDsFor(spec)
	lanes := map[string]any{}
	// #288: report every lane command the lane set needs in a single error with a
	// JSON-array example, instead of surfacing them one round-trip at a time.
	var missing []string
	for _, laneID := range ids {
		if _, ok := spec.Lanes[laneID]; !ok {
			missing = append(missing, laneID)
		}
	}
	if len(missing) > 0 {
		return nil, &Error{
			Message:   fmt.Sprintf("lane_set %q requires lane command(s): %s", spec.LaneSet, strings.Join(missing, ", ")),
			FieldPath: "spec.lanes",
			Hint:      "supply each lane command as a JSON string array, e.g. " + laneCommandOptionExamples(ids),
		}
	}
	for _, laneID := range ids {
		body := spec.Lanes[laneID]
		command, err := stringList(body["command"], "spec.lanes."+laneID+".command")
		if err != nil || len(command) == 0 {
			return nil, &Error{
				Message:   fmt.Sprintf("lane %q command must be a non-empty JSON string array", laneID),
				FieldPath: "spec.lanes." + laneID + ".command",
				Hint:      "e.g. " + laneCommandOptionExample(laneID),
			}
		}
		display := fmt.Sprint(body["display_model"])
		if display == "" || display == "<nil>" {
			display = laneID
		}
		adapter := fmt.Sprint(body["adapter"])
		if adapter == "" || adapter == "<nil>" {
			adapter = "process"
		}
		caps := []string{"write", "review", "synthesis"}
		if rawCaps, ok := body["capabilities"]; ok {
			caps, err = stringList(rawCaps, "spec.lanes."+laneID+".capabilities")
			if err != nil {
				return nil, err
			}
		}
		lane := map[string]any{
			"adapter":       adapter,
			"display_model": display,
			"command":       command,
			"capabilities":  caps,
		}
		for _, key := range []string{"adapter_capabilities", "supervision", "path_prefix", "command_env"} {
			if value, ok := body[key]; ok {
				lane[key] = value
			}
		}
		if err := workflowauthoring.RefuseRetiredOneShotLane(laneID, lane); err != nil {
			return nil, genErr(err.Error(), "spec.lanes."+laneID+".command")
		}
		defaultAgentLoopLane(command, lane)
		lanes[laneID] = lane
	}
	repoWrite := map[string]struct{}{}
	for _, id := range ids {
		if !strings.Contains(id, "reviewer") {
			repoWrite[id] = struct{}{}
		}
	}
	return applyLaneModifiers(spec, lanes, repoWrite)
}

func defaultAgentLoopLane(command []string, lane map[string]any) {
	if !defaultAgentLoopCommand(command) || laneDisablesAgentLoop(lane) {
		return
	}
	caps, _ := lane["adapter_capabilities"].(map[string]any)
	if caps == nil {
		caps = map[string]any{}
	} else {
		caps = cloneMap(caps)
	}
	if _, exists := caps["agent_loop"]; !exists {
		caps["agent_loop"] = true
	}
	lane["adapter_capabilities"] = caps

	supervision, _ := lane["supervision"].(map[string]any)
	if supervision == nil {
		supervision = map[string]any{}
	} else {
		supervision = cloneMap(supervision)
	}
	if _, exists := supervision["transport"]; !exists {
		supervision["transport"] = "pty_helper"
	}
	lane["supervision"] = supervision
}

func defaultAgentLoopCommand(command []string) bool {
	if len(command) == 0 {
		return false
	}
	adapter := strings.TrimSuffix(path.Base(strings.TrimSpace(command[0])), ".exe")
	switch adapter {
	case "agy", "claude":
		return true
	case "codex":
		return !codexExecCommand(command)
	default:
		return false
	}
}

func codexExecCommand(command []string) bool {
	for _, arg := range command[1:] {
		if arg == "exec" {
			return true
		}
	}
	return false
}

func laneDisablesAgentLoop(lane map[string]any) bool {
	if value, ok := lane["agent_loop"].(bool); ok {
		return !value
	}
	caps, _ := lane["adapter_capabilities"].(map[string]any)
	if value, ok := caps["agent_loop"].(bool); ok {
		return !value
	}
	return false
}

// laneCommandOptionExample renders a copy-pasteable `--option lanes.<id>.command`
// flag with a JSON string-array value (#288), so the generator's lane errors show
// the exact shape the operator must pass.
func laneCommandOptionExample(laneID string) string {
	return fmt.Sprintf(`--option 'lanes.%s.command=["claude","--dangerously-skip-permissions"]'`, laneID)
}

func laneCommandOptionExamples(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, laneCommandOptionExample(id))
	}
	return strings.Join(parts, " ")
}

func laneIDsFor(spec Spec) []string {
	switch spec.LaneSet {
	case "single_agent":
		return []string{"agent"}
	case "author_reviewer":
		return []string{"author", "reviewer"}
	case "multi_review":
		result := []string{"author"}
		for idx := 1; idx <= reviewerCount(spec); idx++ {
			result = append(result, fmt.Sprintf("reviewer_%d", idx))
		}
		return result
	case "custom":
		keys := []string{}
		for key := range spec.Lanes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	default:
		return []string{"local"}
	}
}

func compileCustom(spec Spec, lanes map[string]any) ([]map[string]any, []map[string]any, []map[string]any, error) {
	if spec.Plan["schema_version"] != PlanSchemaVersion {
		return nil, nil, nil, genErr("custom plan has unsupported schema_version", "spec.plan.schema_version")
	}
	blocks, err := objectList(spec.Plan["blocks"], "spec.plan.blocks")
	if err != nil {
		return nil, nil, nil, err
	}
	bindings, err := object(defaultAny(spec.Plan["job_lane_bindings"], map[string]any{}), "spec.plan.job_lane_bindings")
	if err != nil {
		return nil, nil, nil, err
	}
	seen := map[string]struct{}{}
	jobs := []map[string]any{}
	for idx, block := range blocks {
		prefix := fmt.Sprintf("spec.plan.blocks[%d]", idx)
		blockID, err := requiredString(block, "id", prefix)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, ok := seen[blockID]; ok {
			return nil, nil, nil, genErr("duplicate custom block id", prefix+".id")
		}
		seen[blockID] = struct{}{}
		kind, err := choice(block, "kind", blockKinds, prefix)
		if err != nil {
			return nil, nil, nil, err
		}
		laneID, ok := bindings[blockID].(string)
		if !ok || laneID == "" {
			return nil, nil, nil, genErr("custom block missing lane binding", "spec.plan.job_lane_bindings."+blockID)
		}
		if _, ok := lanes[laneID]; !ok {
			return nil, nil, nil, genErr("custom lane binding references missing lane", "spec.plan.job_lane_bindings."+blockID)
		}
		if _, ok := block["review_posture"]; ok && !isReviewKind(kind) {
			return nil, nil, nil, genErr("review-only fields on non-review block", prefix+".review_posture")
		}
		artifactPath := fmt.Sprintf("%s/%s.md", spec.ArtifactRoot, strings.ToUpper(blockID))
		if value, ok := block["artifact_path"].(string); ok && value != "" {
			artifactPath = value
		}
		if err := safeArtifactPath(artifactPath, spec.ArtifactRoot, prefix+".artifact_path"); err != nil {
			return nil, nil, nil, err
		}
		custom, err := customJob(blockID, kind, laneID, artifactPath, block, idx)
		if err != nil {
			return nil, nil, nil, err
		}
		jobs = append(jobs, custom)
	}
	ids := map[string]struct{}{}
	for _, job := range jobs {
		ids[fmt.Sprint(job["id"])] = struct{}{}
	}
	edges, err := customEdges(spec.Plan, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := assertAcyclic(ids, edges); err != nil {
		return nil, nil, nil, err
	}
	cycles, err := customCycles(spec.Plan, ids, jobs)
	if err != nil {
		return nil, nil, nil, err
	}
	return jobs, edges, cycles, nil
}

func job(id, jobType, title, role, lane, root, filename, artifactKind, logicalName, prompt, objective string) map[string]any {
	if objective == "" {
		objective = title
	}
	return map[string]any{
		"id":          id,
		"type":        jobType,
		"title":       title,
		"role_id":     role,
		"lane_id":     lane,
		"objective":   objective,
		"task_prompt": map[string]any{"path": "prompts/" + prompt + ".md"},
		"write_scope": map[string]any{"mode": "repo_write", "repo_write": true, "allowed_paths": []string{root + "/"}, "forbidden_paths": []string{".striatum/"}},
		"expected_artifacts": []map[string]any{{
			"logical_name": logicalName,
			"kind":         artifactKind,
			"path":         root + "/" + filename,
			"required":     true,
			"placement":    generatedArtifactPlacement(jobType, artifactKind),
		}},
	}
}

func generatedArtifactPlacement(jobType, artifactKind string) string {
	switch jobType {
	case "synthesis", "phase_synthesis":
		return artifactcontracts.PlacementGitPublication
	default:
		return artifactcontracts.DefaultPlacementForKind(artifactKind)
	}
}

func reviewJob(id, lane, artifactPath, posture string) map[string]any {
	return reviewJobForRole(id, "Review the draft", "reviewer", lane, artifactPath, posture, "review", "review", "Review the draft and record a finding.")
}

func reviewJobForRole(id, title, role, lane, artifactPath, posture, logicalName, prompt, objective string) map[string]any {
	root := path.Dir(artifactPath)
	filename := path.Base(artifactPath)
	result := job(id, "review", title, role, lane, root, filename, "finding", logicalName, prompt, objective)
	result["fresh_session_required"] = true
	result["write_scope"] = map[string]any{"mode": "review_only_artifact", "repo_write": false, "allowed_paths": []string{root + "/"}, "forbidden_paths": []string{".striatum/"}}
	if posture != "neutral" {
		result["review_posture"] = posture
	}
	return result
}

func renderFiles(spec Spec, workflow map[string]any, roles map[string]any) ([]map[string]any, error) {
	body, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		return nil, err
	}
	files := []map[string]any{{"path": spec.ScaffoldRoot + "/workflow.json", "content": string(body) + "\n"}}
	for _, role := range sortedMapKeys(roles) {
		files = append(files, map[string]any{"path": spec.ScaffoldRoot + "/roles/" + role + ".md", "content": roleStub(role)})
	}
	prompts := map[string]struct{}{}
	for _, item := range listFrom(workflow["jobs"]) {
		job := mapFrom(item)
		task := mapFrom(job["task_prompt"])
		if p, ok := task["path"].(string); ok && strings.HasPrefix(p, "prompts/") {
			prompts[strings.TrimPrefix(p, "prompts/")] = struct{}{}
		}
	}
	for _, prompt := range sortedKeys(prompts) {
		files = append(files, map[string]any{"path": spec.ScaffoldRoot + "/prompts/" + prompt, "content": promptStub(prompt)})
	}
	return files, nil
}

func ValidateWorkflow(workflow map[string]any) error {
	if err := workflowauthoring.Validate(workflow); err != nil {
		if authoringErr, ok := err.(*workflowauthoring.Error); ok {
			fieldPath := authoringErr.FieldPath
			if fieldPath != "" && !strings.HasPrefix(fieldPath, "workflow.") {
				fieldPath = "workflow." + fieldPath
			}
			return genErr(authoringErr.Message, fieldPath)
		}
		return err
	}
	if err := workflowauthoring.RefuseRetiredOneShotLanes(workflow); err != nil {
		return genErr(err.Error(), "workflow.lanes")
	}
	return nil
}

func SafeRelativePath(value, fieldPath string) (string, error) {
	return safeRelativePath(value, fieldPath, false)
}

// SafeScaffoldRoot validates a scaffold_root path. Unlike SafeRelativePath it
// permits paths under `.striatum/scratch/` (#288): the scaffold (workflow.json +
// prompts) is throwaway operator input that `run prepare` snapshots anyway, and
// `.striatum/scratch/` is the product-boundary home for operational scratch, so
// targeting it avoids forcing the scaffold into a tracked path that then needs
// cleanup. Every other `.striatum/` subdirectory, `.git`, and any traversal
// outside the repository stay rejected.
func SafeScaffoldRoot(value, fieldPath string) (string, error) {
	return safeRelativePath(value, fieldPath, true)
}

func safeRelativePath(value, fieldPath string, allowStriatumScratch bool) (string, error) {
	if value == "" || strings.ContainsRune(value, '\x00') || strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
		return "", genErr("path must be repo-relative", fieldPath)
	}
	clean := path.Clean(value)
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." {
		return "", genErr("path must not escape the repository or target .git/.striatum", fieldPath)
	}
	parts := strings.Split(clean, "/")
	// Operator scratch is allowed only under `.striatum/scratch/<...>` — never the
	// `.striatum` root itself, and never another `.striatum/` subdirectory.
	scratchRoot := allowStriatumScratch && len(parts) >= 2 && parts[0] == ".striatum" && parts[1] == "scratch"
	for index, part := range parts {
		if part == ".." || part == ".git" {
			return "", genErr("path must not escape the repository or target .git/.striatum", fieldPath)
		}
		if part == ".striatum" && (index != 0 || !scratchRoot) {
			return "", genErr("path must not escape the repository or target .git/.striatum", fieldPath)
		}
	}
	return strings.TrimSuffix(clean, "/"), nil
}

func FileHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func roleStub(role string) string {
	panelRoles := map[string]string{
		"problem_framer":    "# Problem Framer Role\n\nYou frame the implementation problem before proposals begin. Publish constraints, goals, non-goals, and decision criteria at the declared artifact path.\n",
		"proposer_a":        "# Proposer A Role\n\nYou develop implementation option A independently from the other proposal roles. Stay inside the declared write scope.\n",
		"proposer_b":        "# Proposer B Role\n\nYou develop implementation option B independently from the other proposal roles. Stay inside the declared write scope.\n",
		"proposer_c":        "# Proposer C Role\n\nYou develop implementation option C independently from the other proposal roles. Stay inside the declared write scope.\n",
		"scorekeeper":       "# Scorekeeper Role\n\nYou score one proposal against the selected adversary-pack dimensions. Publish only the review artifact at the declared path.\n",
		"tradeoff_ledger":   "# Tradeoff Ledger Role\n\nYou normalize proposal and scorecard evidence into a tradeoff ledger at the declared artifact path.\n",
		"arbitrator":        "# Arbitrator Role\n\nYou select or compose the preferred implementation path from the tradeoff ledger and supporting evidence.\n",
		"dissent_reviewer":  "# Dissent Reviewer Role\n\nYou try to falsify the arbitration before final decision. Publish only the review artifact at the declared path.\n",
		"principal_decider": "# Principal Decider Role\n\nYou record the final implementation decision and required follow-up work at the declared artifact path.\n",
		"holder":            "# Holder Role\n\nYou publish the leading proposal as the claim falsifiers will challenge. Do not wait for live questions; the adjudicator ledger decides whether the static challenge/rebuttal gate clears.\n",
		"falsifier":         "# Falsifier Role\n\nYou challenge the published holder artifact. Write a concrete falsifying gap, the strongest rebuttal you can justify from the available artifacts, and do not publish the collaboration ledger.\n",
		"cross_examiner":    "# Cross-Examiner Role\n\nYou challenge the published finding or proposal before downstream publication. Record the challenge, the strongest rebuttal you can justify, and any unanswered gap in your declared artifact.\n",
		"adjudicator":       "# Adjudicator Role\n\nYou read only the curated dialogue trajectory, never raw terminal output. Publish the collaboration ledger and verdict according to the substance rubric. The `verdict` field MUST be one of: accept, accept_with_findings, needs_revision, reject. A clearing verdict (the one that lets the downstream phase publish) is `accept` or `accept_with_findings` — do not write `clear` or any other value.\n",
		"scribe":            "# Scribe Role\n\nYou record only the decision trail visible in the dialogue trajectory. Do not hypothesize, infer hidden reasoning, or add claims that are not present in the curated dialogue.\n",
		"committer":         "# Committer Role\n\nYou publish the downstream proposal or finding only after the collaboration ledger verdict clears the phase gate.\n",
		"convener":          "# Convener Role\n\nYou frame the problem, draft the candidate synthesis, and stay live for cross-examination. On a revision cycle you receive the prior cycle's constraints[] as binding input and must discharge each row explicitly. Do not treat dialogue completion as acceptance; the adjudicator ledger decides whether the gate clears.\n",
		"revision_convener": "# Revision Convener Role\n\nYou republish the synthesis after an adjudicated needs_revision. You take the prior cycle's constraints[] as first-class input and discharge each row explicitly (answer / fold-in / reject-with-rationale / accept-as-risk / defer-with-successor). Republished artifacts use the cycle-templated logical name.\n",
		"spec_author":       "# Spec Author Role\n\nYou write the RFC/spec using the latest cleared constraint ledger as binding input, not the original proposal. Every binding constraint must land in the spec as testable text or a gate.\n",
		"final_reviewer":    "# Final Reviewer Role\n\nYou verify discharge, you do not re-run the forum. Emit a constraint_discharge table marking each binding constraint discharged / partial / missing / accepted_risk with evidence. Final review is a typecheck that fails closed on any undischarged binding constraint.\n",
		"diverger":          "# Diverger Role\n\nYou generate ideas under one assigned cognitive frame, in DIVERGENT mode only. Produce short, distinct ideas; do not evaluate, rank, or hedge, and do not read other branches. The first three obvious answers are banned — push into the awkward middle. Publish only your branch artifact at the declared path.\n",
		"convergence_critic": "# Convergence Critic Role\n\nYou are the critic. Read every divergence branch, score each idea on novelty/viability/fit, cluster by underlying angle, flag traps with reasons, and select the top picks by weighted score. Note ideas independently surfaced by branches on different model families (cross-model agreement). Publish only the convergence ledger at the declared path.\n",
		"deepener":          "# Deepener Role\n\nYou take one surviving pick and connect the dots: a 4-8 sentence sketch of how it works, the load-bearing risk, the first concrete step a builder would take, and 3-5 child ideas. Publish only your deepened artifact at the declared path.\n\nYour artifact is a `striatum.synthesis.v1` document. Every deepen lane must emit identical front-matter shape so the artifacts are uniform across models: include an `author:` line (your `<role-name>-<model-name>-<ordinal>` byline) and a complete `inputs:` list naming BOTH upstream artifacts you consumed — the convergence ledger (`CONVERGENCE.md`) AND the problem brief (`PROBLEM_BRIEF.md`). Do not omit `author:` and do not list only the convergence ledger.\n",
		"final_synthesizer": "# Final Synthesizer Role\n\nYou assemble the operator-facing result: the shortlist with rationale, the non-obvious-but-viable pick marked with a star, the trap list, and one wildcard provocation. Publish only the final synthesis at the declared path.\n",
		"coordinator":       "# Coordinator Role\n\nYou orchestrate a collaboration shape (fog-of-war or synaptic-prune): you partition context, open and close the conversation/interrogation cycles, and stage the curated trajectory for the judge/adjudicator. For synaptic_prune you open the conversation with a post_dialog_hook so close emits the prune fan-out BEFORE participant teardown, then nominate against still-live participants. You do not score substance — the judge/adjudicator ledger decides whether the gate clears. Carry session ids and trajectory references, never raw provider output.\n",
		"reconstructor":     "# Reconstructor Role\n\nYou hold exactly ONE disjoint fragment of the spec. You must interrogate your peers to recover the constraints you were NOT given, and record which constraints you reconstructed (citing the peer turn that revealed each) versus which you could not. Never invent a constraint you cannot source from a peer answer — the full-spec judge scores hallucinations as failures. Stay live for interrogation by the rollup.\n",
		"judge":             "# Judge Role\n\nYou alone hold the FULL spec (the ground truth). You read only the curated reconstruction trajectory and score each hidden constraint reconstructed / hallucinated / missed; you publish the collaboration ledger verdict. A lane that claimed coverage it never reconstructed scores hallucinated/missed and you return needs_revision; the proposal stays withheld until your verdict clears. The `verdict` field MUST be one of: accept, accept_with_findings, needs_revision, reject.\n",
		"proposer":          "# Proposer Role\n\nYou author the proposal — but only after the coverage gate cleared (the work-packet type sequencing withholds you until then). Build on the reconstructed constraints the judge confirmed, never on a constraint a reconstructor hallucinated.\n",
		"pruner":            "# Pruner Role\n\nYou are a forum participant. While still live in your preserved-context window, you nominate exactly ONE claim from the forum to retire ('do not re-litigate'), with a coherent rationale. A claim is retired only if at least two participants independently nominate it. You do not tally — the adjudicator ledger records the ≥2-vote retirements.\n",
		"builder":           "# Builder Role\n\nYou build the slice and publish a claim ledger naming every capability claim with a status (VERIFIED|ASSERTED|DESIGNED) and a stable id. You do NOT decide whether a claim is verified — the verify step runs the sanctioned checks and the adjudicator reads the receipts. Do not state a claim above the status its check can earn. You cannot author the sanctioned check set (the verifier intent is in your peer's forbidden_paths, not yours).\n",
		"verifier":          "# Verifier Role\n\nYou run `striatum verifier run` against the sanctioned checks and publish the minted receipts as ground truth — never the builder's prose. The builtin checks (builtin:go-test/vet/build, artifact-anchor-integrity) need zero operator JSON and cap their claims at ASSERTED; VERIFIED is reserved for an external check the operator has pinned AND attested. You MUST NOT edit verification/allowlist.intent.json (it is in your forbidden_paths): a verified lane can never sanction its own checks. A check whose negative control unexpectedly passes voids the receipt — report it RED.\n",
	}
	if content, ok := panelRoles[role]; ok {
		return content
	}
	if role == "reviewer" {
		return "# Reviewer Role\n\nYou are the reviewer for this workflow. Read the upstream draft and write a single review-only finding artifact at the declared path; do not modify other files.\n"
	}
	return "# Author Role\n\nYou are the author for this workflow. Produce the expected handoff artifact at the path declared in the workflow. Stay inside the declared write scope.\n"
}

func promptStub(prompt string) string {
	switch prompt {
	case "draft.md":
		return "Draft the initial artifact described by the workflow. Replace this stub with the concrete authoring instructions for your team.\n"
	case "review.md":
		return "Review the upstream draft and record a finding with one of the supported verdicts. Replace this stub with reviewer guidance.\n"
	case "apply.md":
		return "Apply the accepted review by producing the final synthesis artifact. Replace this stub with concrete apply instructions.\n"
	case "collaboration_holder.md":
		return "Produce the leading proposal as the published claim falsifiers will challenge. Do not treat challenge completion as acceptance; the adjudicator ledger decides whether the gate clears.\n"
	case "collaboration_falsifier.md":
		return "Read the published holder proposal and write a material falsifying challenge. Record the challenge, the strongest rebuttal you can justify, and any unanswered gap in the declared artifact.\n"
	case "collaboration_author_draft.md":
		return "Draft the finding or proposal as the published claim cross-examiners will challenge. The downstream publication is gated by the adjudicator's collaboration ledger.\n"
	case "collaboration_cross_examiner.md":
		return "Read the published draft and write one falsifying cross-examination challenge. Record the challenge, the strongest rebuttal you can justify, and any unanswered gap in the declared artifact.\n"
	case "adjudicate_collaboration.md":
		return "Read only the curated dialogue trajectory. Publish a collaboration_ledger whose verdict reflects whether a material challenge landed and was directly rebutted.\n"
	case "collaboration_scribe.md":
		return "Record only the visible dialogue decision trail. Do not invent missing reasoning or copy raw terminal/provider output.\n"
	case "collaboration_commit.md":
		return "Publish the downstream proposal or finding after the adjudicator ledger verdict clears the phase gate.\n"
	case "collaboration_final_summary.md":
		return "Summarize the collaboration gate result and downstream publication in a final synthesis artifact.\n"
	case "ace_survey.md":
		return "Survey the prior art, evidence, and existing constraints for the topic. Record what is already known and what is contested; do not synthesize a solution yet.\n"
	case "ace_survey_synthesis.md":
		return "Frame the problem, goals, non-goals, and decision criteria from the survey. This phase synthesis sets the scope the candidate synthesis must address.\n"
	case "ace_convener.md":
		return "Draft the candidate synthesis and stay live for cross-examination. On a revision cycle you receive the prior cycle's constraints[] as binding input; discharge each row explicitly. Do not treat dialogue completion as acceptance.\n"
	case "ace_convener_synthesis.md":
		return "Publish the candidate synthesis. On a revision cycle, every prior constraints[] row must be discharged explicitly (answer / fold-in / reject-with-rationale / accept-as-risk / defer-with-successor). This artifact is cycle-templated so it republishes cleanly.\n"
	case "ace_cross_examiner.md":
		return "Challenge the candidate synthesis from your assigned posture only (product / implementation / privacy / eval / operations or the configured override). Record findings[] rows with severity, the affected invariant, the closest acceptable answer, and the constraint shape you would require. An unanswered interrogation is evidence — record it.\n"
	case "ace_cross_exam_synthesis.md":
		return "Roll up every cross-examiner posture into one findings ledger. Preserve each finding's posture, severity, and status; carry unanswered interrogations forward as evidence for the adjudicator.\n"
	case "ace_adjudication_intake.md":
		return "Assemble the candidate synthesis and the cross-examination findings for adjudication. Do not add new challenges; stage the curated trajectory only.\n"
	case "ace_adjudicate.md":
		return "Read only the curated trajectory. Publish the collaboration_ledger verdict (accept / accept_with_findings / needs_revision / reject). On needs_revision you MUST convert each load-bearing challenge into a binding constraints[] row (or an explicit unresolved_question row); a naked refusal with an empty constraints[] is rejected (exit code 6). Each binding constraint needs a typed kind, a source_finding, a posture, severity, and a verification gate or expected_stage. Maintain the posture-disposition matrix in branches{}.\n"
	case "ace_revision_convener.md":
		return "Take the prior cycle's constraints[] as binding input. Discharge each row explicitly: answer / fold-in / reject-with-rationale / accept-as-risk / defer-with-successor. A high-severity challenge may only leave open via a recorded disposition. Republished artifacts use the cycle-templated logical name.\n"
	case "ace_revision_synthesis.md":
		return "Publish the revised synthesis that discharges the adjudicated constraints[]. Each binding constraint must be visibly addressed; this phase synthesis is the candidate the discharge review checks.\n"
	case "ace_discharge_review.md":
		return "Review the revised synthesis against the binding constraints[]. Confirm each constraint is discharged or flag it still open; this re-review is cycle-templated so each cycle republishes cleanly.\n"
	case "ace_discharge_review_synthesis.md":
		return "Confirm the latest cleared constraint ledger before spec publication. Record which ledger cycle is binding for the spec author.\n"
	case "ace_spec_author.md":
		return "Write the RFC/spec from the latest cleared constraint ledger as binding input — not from the original proposal. Every binding constraint must land in the spec as testable text or a gate.\n"
	case "ace_spec_publication.md":
		return "Publish the spec gated on the latest cleared collaboration ledger. The spec begins from adjudicated constraints, not the original proposal.\n"
	case "ace_final_review.md":
		return "Emit a constraint_discharge table: for each binding constraint, mark discharged / partial / missing / accepted_risk with evidence (a spec section or gate reference). Final review is a typecheck — do not re-run the forum. It fails closed on any binding constraint that is missing or partial-without-accepted-risk.\n"
	case "ace_final_review_synthesis.md":
		return "Summarize the discharge typecheck. The run fails closed on any undischarged binding constraint; record the coverage counts (raised / converted / discharged) for the dashboard.\n"
	case "frame_problem.md":
		return "Publish a concise problem brief: the open-ended question to ideate on, plus constraints, goals, non-goals, and decision criteria. Do not propose solutions — only frame the space the divergence branches will explore.\n"
	case "diverge.md":
		return "Generate short, distinct ideas under your assigned cognitive frame in DIVERGENT mode. Do not evaluate, rank, or hedge; you cannot see the other branches. The first three obvious answers are banned. Replace this stub with the frame's vantage from your objective.\n"
	case "converge.md":
		return "Read every divergence branch. Score each idea on novelty/viability/fit, cluster by underlying angle, flag traps with reasons, select the top-K picks, and note any idea independently surfaced across different model families. Publish the scored, clustered convergence ledger.\n"
	case "deepen.md":
		return "Take the assigned ranked pick from the convergence ledger and deepen it: a 4-8 sentence sketch, the load-bearing risk, the first concrete step, and 3-5 child ideas.\n\nEmit a `striatum.synthesis.v1` artifact with uniform front matter so every deepen lane (across models) is identical in shape: set the `author:` front-matter line to your `<role-name>-<model-name>-<ordinal>` byline, and set a complete `inputs:` list naming BOTH `CONVERGENCE.md` (the convergence ledger) and `PROBLEM_BRIEF.md` (the problem brief). Do not put `author:` only in the body, and do not list only the convergence ledger.\n"
	case "final_synthesis.md":
		return "Assemble the operator-facing result from the deepened picks and the convergence ledger: shortlist with rationale, the non-obvious-but-viable pick marked with a star, the trap list, and one wildcard provocation.\n"
	case "fog_fragment_scan.md":
		return "Partition the spec into disjoint fragments — one per reconstructor lane. Each lane receives ONLY its fragment; the full spec is held only by the judge. Record the partition so each reconstructor's hidden constraints are well-defined.\n"
	case "fog_fragment_map.md":
		return "Publish the fixed fragment-to-lane map. Frame which constraints each reconstructor must recover THROUGH peer interrogation, not from its own fragment. The partition is fixed at prepare time.\n"
	case "fog_reconstructor.md":
		return "You hold ONE disjoint fragment of the spec. Interrogate your peers to recover the constraints you were NOT given, and record which constraints you reconstructed (with the peer turn that revealed each) vs which you could not. Do not invent constraints you cannot source from a peer answer. Stay live for interrogation.\n"
	case "fog_reconstruction_rollup.md":
		return "Roll up every reconstructor's recovered-constraint record into one findings ledger. Interrogate each reconstructor to confirm which constraints it actually reconstructed; carry unanswered interrogations forward as evidence for the coverage judge.\n"
	case "fog_coverage_intake.md":
		return "Assemble the reconstruction rollup and the full spec for coverage adjudication. Do not add new constraints; stage the curated trajectory only.\n"
	case "fog_coverage_gate.md":
		return "You alone hold the full spec. Read only the curated reconstruction trajectory and score each hidden constraint reconstructed / hallucinated / missed against your ground truth; publish the collaboration_ledger (shape fog_of_war_review) verdict. A lane that CLAIMED coverage it never reconstructed scores hallucinated/missed → needs_revision; the proposal stays withheld until this verdict clears.\n"
	case "fog_proposal.md":
		return "Author the proposal only after the coverage gate cleared (work-packet type sequencing withholds you until then). Build on the reconstructed constraints the judge confirmed, not on any constraint a reconstructor hallucinated.\n"
	case "fog_proposal_synthesis.md":
		return "Summarize the cleared coverage gate and the published proposal.\n"
	case "prune_forum_open.md":
		return "Open a round-robin conversation via conversation.open, declaring a post_dialog_hook {deliver_to: <coordinator session>, packet_type: prune} so close emits the prune packet BEFORE the participants' preserved-context window releases (RFC 0094 §1). Run the discussion; record the transcript reference, not raw provider output.\n"
	case "prune_forum_close.md":
		return "Close the conversation. On close the post_dialog_hook emits exactly one prune packet to the coordinator carrying the participant session ids + the transcript ref, before any participant teardown. Publish the close summary referencing the curated dialogue trajectory.\n"
	case "prune_nominator.md":
		return "While still live in your preserved-context window, nominate exactly ONE claim from the forum to retire ('do not re-litigate'), with a coherent rationale. A claim is only retired if ≥2 nominators independently pick it.\n"
	case "prune_nomination_rollup.md":
		return "Gather every still-live participant's prune nomination. If a target participant already died, RECORD the dead target and continue (do not hang) — clean refusal. Carry each nomination + rationale forward to the tally.\n"
	case "prune_tally_intake.md":
		return "Assemble the nominations for the prune tally. Stage the curated trajectory only.\n"
	case "prune_tally.md":
		return "Read only the curated nomination trajectory. Retire every claim nominated by ≥2 participants with coherent rationale as a nomination-kind entry in the collaboration_ledger (shape synaptic_prune); publish the verdict. The retired set is the durable NEGATIVE PREAMBLE ('do not re-litigate: …') injected into future runs on the same topic — provenance, not reputation.\n"
	case "claim_build.md":
		return "Build the slice and publish a claim_ledger. Give every capability claim a stable id, a status (VERIFIED|ASSERTED|DESIGNED), and (above DESIGNED) the id of the sanctioned check that substantiates it. Do not state a claim above the status its check can earn; deferral is a DESIGNED row, never hidden prose. You cannot author the sanctioned check set.\n"
	case "verify_run.md":
		return "Run `striatum verifier run` against the sanctioned checks and publish the minted receipts. The builtin checks (builtin:go-test/vet/build, artifact-anchor-integrity) run with no operator JSON and cap their claims at ASSERTED; VERIFIED needs an external check the operator has pinned (`striatum verifier pin --host-here`) AND attested. Receipts come from the engine's exit codes, not the builder's prose. Do NOT edit verification/allowlist.intent.json — it is in your forbidden_paths. A check whose negative control passes voids the receipt: report it RED.\n"
	case "adjudicate.md":
		return "Read the claim ledger and the minted receipts and publish the collaboration_ledger verdict. If ANY claim is stated above the status its receipt earns — VERIFIED over a missing/RED receipt, or completion language over an ASSERTED/DESIGNED row — record needs_revision and name the offending claims. Otherwise accept.\n"
	case "commit_verified.md":
		return "Publish the cleared release only after the collaboration ledger records an accepting verdict. Stamp every claim with its earned status and the receipt of record; no completion language survives above the receipted status.\n"
	default:
		return fmt.Sprintf("Complete the %s step declared by the workflow.\n", strings.ReplaceAll(strings.TrimSuffix(prompt, ".md"), "_", " "))
	}
}

func validateModifierMatrix(spec Spec, warnings *[]string) error {
	if isCollaborationShape(spec.Shape) && spec.LaneSet == "single_agent" {
		return &Error{Message: "collaboration shapes require at least a fixture or independent adjudication lane set", FieldPath: "spec.lane_set", Hint: "Use lane_set local for fixtures or author_reviewer/multi_review for real runs."}
	}
	for idx, modifier := range spec.LaneModifiers {
		if (modifier == "supervised" || modifier == "harness_profiled") && spec.LaneSet == "local" {
			return &Error{Message: "lane modifier is incompatible with lane set", FieldPath: fmt.Sprintf("spec.lane_modifiers[%d]", idx), Hint: fmt.Sprintf("modifier %q is forbidden for lane_set 'local'", modifier)}
		}
		if modifier == "worktree_isolated" && spec.Shape == "multi_review_synthesis" {
			*warnings = append(*warnings, "worktree_isolated has no effect on review-only jobs except synthesis")
		}
	}
	if hasModifier(spec, "harness_profiled") {
		if _, err := harnessProfiles(spec); err != nil {
			return err
		}
	}
	if hasModifier(spec, "constrained") {
		if _, err := constraints(spec); err != nil {
			return err
		}
		if _, err := requiredEnforcement(spec); err != nil {
			return err
		}
	}
	return nil
}

func applyLaneModifiers(spec Spec, lanes map[string]any, repoWrite map[string]struct{}) (map[string]any, error) {
	result := cloneNested(lanes)
	if hasModifier(spec, "worktree_isolated") {
		for laneID := range repoWrite {
			lane := mapFrom(result[laneID])
			lane["worktree_isolation"] = "per_job"
			result[laneID] = lane
		}
	}
	// #288: a supervised/agent-loop repo-write lane structurally requires per-job
	// worktree isolation (the #242 launch gate, RefuseAutonomousSharedCheckoutRepoWrite).
	// Apply it by default for generated repo-write lanes so a real-agent
	// `single_agent`/`author_reviewer` code_change scaffold validates out of the box
	// instead of demanding a hand-edit. The `local` fixture lane (process adapter,
	// no supervision) and review-only lanes are unaffected. The shared-checkout
	// compatibility override is not expressible through the generator spec
	// (compileLanes forwards only a fixed lane-key allowlist), so generated
	// repo-write lanes always take per-job isolation; hand-edit the workflow.json to
	// opt into shared checkout.
	for laneID := range repoWrite {
		lane := mapFrom(result[laneID])
		if workflowauthoring.LaneRequiresWorktreeIsolationForAutonomousRepoWrite(lane) {
			lane["worktree_isolation"] = "per_job"
			result[laneID] = lane
		}
	}
	if hasModifier(spec, "constrained") {
		constraints, err := constraints(spec)
		if err != nil {
			return nil, err
		}
		required, err := requiredEnforcement(spec)
		if err != nil {
			return nil, err
		}
		for laneID, raw := range result {
			lane := mapFrom(raw)
			lane["constraints"] = constraints
			if len(required) > 0 {
				lane["required_enforcement"] = required
			}
			result[laneID] = lane
		}
	}
	if hasModifier(spec, "harness_profiled") {
		profiles, err := harnessProfiles(spec)
		if err != nil {
			return nil, err
		}
		profileIDs := sortedMapKeys(profiles)
		laneIDs := sortedMapKeys(result)
		for idx, laneID := range laneIDs {
			pick := idx
			if pick >= len(profileIDs) {
				pick = len(profileIDs) - 1
			}
			lane := mapFrom(result[laneID])
			lane["harness_profile_id"] = profileIDs[pick]
			result[laneID] = lane
		}
	}
	return result, nil
}

// applyRepoWriteWorktreeIsolation guarantees that every lane carrying an
// autonomous/supervised repo-write job gets worktree_isolation: per_job (#301).
// It mutates the shared `lanes` map in place. The repo-write lane set is derived
// from the compiled jobs (write_scope.repo_write / mode == "repo_write") rather
// than a lane-name heuristic, so it covers every lane the job graph actually
// assigns repo-write work to — including a lane named `reviewer` that a fan-out
// shape (e.g. divergent_ideation) round-robins diverge/deepen jobs onto. This is
// the same per-job repo-write signal workflowauthoring.RefuseAutonomousShared-
// CheckoutRepoWrite enforces at validate/prepare, so generate cannot emit output
// that its own validator rejects.
func applyRepoWriteWorktreeIsolation(lanes map[string]any, jobs []map[string]any) {
	for _, job := range jobs {
		if !jobDeclaresRepoWrite(job) {
			continue
		}
		laneID, ok := job["lane_id"].(string)
		if !ok || laneID == "" {
			continue
		}
		raw, ok := lanes[laneID]
		if !ok {
			continue
		}
		lane := mapFrom(raw)
		if workflowauthoring.LaneRequiresWorktreeIsolationForAutonomousRepoWrite(lane) {
			lane["worktree_isolation"] = "per_job"
			lanes[laneID] = lane
		}
	}
}

// jobDeclaresRepoWrite mirrors workflowauthoring.jobIsRepoWrite: a job is
// repo-write when its write_scope sets repo_write=true or mode == "repo_write".
func jobDeclaresRepoWrite(job map[string]any) bool {
	scope, ok := job["write_scope"].(map[string]any)
	if !ok {
		return false
	}
	if scope["repo_write"] == true {
		return true
	}
	mode, _ := scope["mode"].(string)
	return mode == "repo_write"
}

func harnessProfiles(spec Spec) (map[string]any, error) {
	profiles, err := object(spec.Options["harness_profiles"], "spec.options.harness_profiles")
	if err != nil || len(profiles) == 0 {
		return nil, genErr("harness_profiled requires options.harness_profiles", "spec.options.harness_profiles")
	}
	result := map[string]any{}
	for profileID, raw := range profiles {
		body, ok := raw.(map[string]any)
		if !ok {
			return nil, genErr("harness profile body must be an object", "spec.options.harness_profiles."+profileID)
		}
		family, _ := body["tool_family"].(string)
		if _, ok := workflowtemplatesHarnessFamilies()[family]; !ok {
			return nil, genErr("harness profile has unknown tool_family", "spec.options.harness_profiles."+profileID+".tool_family")
		}
		result[profileID] = enrichHarnessProfile(cloneMap(body))
	}
	return result, nil
}

func enrichHarnessProfile(body map[string]any) map[string]any {
	fragment := harnessFragmentByToolFamily(fmt.Sprint(body["tool_family"]))
	if fragment == nil {
		return body
	}
	native := map[string]any{}
	if raw, ok := body["native_delegation"].(map[string]any); ok {
		native = cloneMap(raw)
	}
	if value, ok := native["instruction"].(string); !ok || strings.TrimSpace(value) == "" {
		native["instruction"] = fragment["native_delegation_instruction"]
	}
	if _, ok := native["mode"]; !ok {
		if mode, ok := fragment["native_delegation_mode"].(string); ok && mode != "" {
			native["mode"] = mode
		}
	}
	body["native_delegation"] = native
	return body
}

func harnessFragmentByToolFamily(family string) map[string]any {
	catalog, err := workflowtemplates.Load()
	if err != nil {
		return nil
	}
	for _, fragment := range catalog.HarnessProfileFragments {
		if fragment["tool_family"] == family {
			return fragment
		}
	}
	return nil
}

func workflowtemplatesHarnessFamilies() map[string]struct{} {
	return set("generic", "codex", "claude_code", "agy")
}

func constraints(spec Spec) (map[string]any, error) {
	constraints, err := object(defaultAny(spec.Options["constraints"], map[string]any{}), "spec.options.constraints")
	if err != nil {
		return nil, err
	}
	for key, value := range constraints {
		allowed, ok := constraintValues[key]
		if !ok {
			return nil, genErr("unknown adapter constraint value", "spec.options.constraints."+key)
		}
		if _, ok := allowed[fmt.Sprint(value)]; !ok {
			return nil, genErr("unknown adapter constraint value", "spec.options.constraints."+key)
		}
	}
	return constraints, nil
}

func requiredEnforcement(spec Spec) (map[string]any, error) {
	required, err := object(defaultAny(spec.Options["required_enforcement"], map[string]any{}), "spec.options.required_enforcement")
	if err != nil {
		return nil, err
	}
	for key, value := range required {
		if _, ok := constraintValues[key]; !ok {
			return nil, genErr("unknown required enforcement value", "spec.options.required_enforcement."+key)
		}
		if _, ok := enforcementLevels[fmt.Sprint(value)]; !ok {
			return nil, genErr("unknown required enforcement value", "spec.options.required_enforcement."+key)
		}
	}
	return required, nil
}

func rolesFor(roleIDs []string) map[string]any {
	result := map[string]any{}
	for _, role := range roleIDs {
		result[role] = map[string]any{"definition_path": "roles/" + role + ".md"}
	}
	return result
}

func coordinator(spec Spec, lanes map[string]any) map[string]any {
	if spec.Shape == "implementation_panel" {
		return map[string]any{"role_id": "problem_framer", "lane_id": panelProposalLane(spec)}
	}
	if spec.Shape == "falsification_gate" {
		return map[string]any{"role_id": "holder", "lane_id": authorLane(spec)}
	}
	if spec.Shape == "cross_examination" {
		return map[string]any{"role_id": "author", "lane_id": authorLane(spec)}
	}
	if spec.Shape == "adjudicated_constraint_extraction" {
		return map[string]any{"role_id": "convener", "lane_id": authorLane(spec)}
	}
	if spec.Shape == "fog_of_war_review" || spec.Shape == "synaptic_prune" {
		return map[string]any{"role_id": "coordinator", "lane_id": authorLane(spec)}
	}
	if spec.Shape == "divergent_ideation" {
		return map[string]any{"role_id": "problem_framer", "lane_id": divergentPrimaryLane(spec)}
	}
	lane := "local"
	if lanes[lane] == nil {
		if lanes["author"] != nil {
			lane = "author"
		} else {
			keys := sortedMapKeys(lanes)
			if len(keys) > 0 {
				lane = keys[0]
			}
		}
	}
	return map[string]any{"role_id": "author", "lane_id": lane}
}

func defaultParallelism(spec Spec) map[string]any {
	maxJobs := 1
	switch spec.Shape {
	case "multi_review_synthesis":
		maxJobs = reviewerCount(spec)
	case "implementation_panel":
		if count, err := panelProposalCount(spec); err == nil {
			maxJobs = count
		}
	case "divergent_ideation":
		if count, err := divergentBranchCount(spec); err == nil {
			maxJobs = count
		}
	}
	return map[string]any{"mode": "declared", "max_active_jobs": maxJobs, "require_disjoint_write_scopes": true}
}

func authorLane(spec Spec) string {
	switch spec.LaneSet {
	case "local":
		return "local"
	case "single_agent":
		return "agent"
	default:
		return "author"
	}
}

func reviewerLane(spec Spec, idx int) string {
	switch spec.LaneSet {
	case "local":
		return "local"
	case "single_agent":
		return "agent"
	case "multi_review":
		return fmt.Sprintf("reviewer_%d", idx)
	default:
		return "reviewer"
	}
}

func reviewerCount(spec Spec) int {
	if value, ok := intFrom(spec.Options["reviewer_count"]); ok && value > 0 {
		return value
	}
	if postures, ok := spec.Options["review_postures"].([]any); ok && len(postures) > 0 {
		return len(postures)
	}
	if isCollaborationShape(spec.Shape) && spec.LaneSet == "multi_review" {
		if spec.Shape == "falsification_gate" {
			if falsifiers, err := falsifierCount(spec); err == nil {
				return falsifiers + 1
			}
			return 3
		}
		return 2
	}
	return 2
}

func postures(spec Spec, count int) ([]string, error) {
	values := []string{"devils_advocate", "security"}
	if raw, ok := spec.Options["review_postures"].([]any); ok && len(raw) > 0 {
		values = []string{}
		for idx, item := range raw {
			posture := fmt.Sprint(item)
			if err := validatePosture(posture, fmt.Sprintf("spec.options.review_postures[%d]", idx)); err != nil {
				return nil, err
			}
			values = append(values, posture)
		}
	}
	for len(values) < count {
		values = append(values, "neutral")
	}
	return values[:count], nil
}

func firstPosture(spec Spec) string {
	if _, ok := spec.Options["review_postures"]; !ok {
		return "neutral"
	}
	values, err := postures(spec, 1)
	if err != nil || len(values) == 0 {
		return "neutral"
	}
	return values[0]
}

func maxCycles(spec Spec) (int, error) {
	value, ok := intFrom(defaultAny(spec.Options["max_revision_cycles"], 1))
	if !ok || value < 1 {
		return 0, genErr("max_revision_cycles must be a positive integer", "spec.options.max_revision_cycles")
	}
	return value, nil
}

func isCollaborationShape(shape string) bool {
	return shape == "falsification_gate" || shape == "cross_examination" ||
		shape == "adjudicated_constraint_extraction" ||
		shape == "fog_of_war_review" || shape == "synaptic_prune"
}

// isPhasedShape reports whether a shape emits a phased striatum.workflow.v1.1
// graph (phases[] + phase_synthesis gate jobs). The collaboration shapes and
// multi_phase carry their collaboration-pack semantics through
// isCollaborationShape; verification_gate (RFC 0141) is phased — its adjudicate
// gate is a phase_synthesis job emitting a cycle-templated collaboration_ledger,
// exactly like the hand-authored verification-gate-flow example — but it is NOT a
// collaboration-pack shape, so it joins here rather than in isCollaborationShape.
func isPhasedShape(shape string) bool {
	return shape == "multi_phase" || isCollaborationShape(shape) || shape == "verification_gate"
}

func validatePosture(posture, fieldPath string) error {
	if _, ok := allowedPostures[posture]; ok {
		return nil
	}
	if strings.HasPrefix(posture, "custom:") && strings.TrimSpace(strings.TrimPrefix(posture, "custom:")) != "" {
		return nil
	}
	return genErr("invalid review posture", fieldPath)
}

func customJob(blockID, kind, laneID, artifactPath string, block map[string]any, idx int) (map[string]any, error) {
	role := "author"
	jobType := kind
	artifactKind := "handoff"
	if isReviewKind(kind) {
		role = "reviewer"
		jobType = "review"
		artifactKind = "finding"
	} else if kind == "synthesis" {
		jobType = "synthesis"
	} else if kind == "implementation" || kind == "test" || kind == "support_ledger" {
		jobType = "build"
		if kind == "support_ledger" {
			artifactKind = "support_ledger"
		}
	}
	title := fmt.Sprint(block["title"])
	if title == "" || title == "<nil>" {
		title = titleFromBlockID(blockID)
	}
	result := job(blockID, jobType, title, role, laneID, path.Dir(artifactPath), path.Base(artifactPath), artifactKind, blockID, kind, "")
	if role == "reviewer" {
		posture := "neutral"
		if value, ok := block["review_posture"].(string); ok {
			posture = value
		} else if value, ok := block["posture"].(string); ok {
			posture = value
		}
		if err := validatePosture(posture, fmt.Sprintf("spec.plan.blocks[%d].posture", idx)); err != nil {
			return nil, err
		}
		result["review_posture"] = posture
		result["fresh_session_required"] = true
	}
	return result, nil
}

func customEdges(plan map[string]any, ids map[string]struct{}) ([]map[string]any, error) {
	raw, err := objectList(defaultAny(plan["edges"], []any{}), "spec.plan.edges")
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for idx, edge := range raw {
		from, err := requiredString(edge, "from", fmt.Sprintf("spec.plan.edges[%d]", idx))
		if err != nil {
			return nil, err
		}
		to, err := requiredString(edge, "to", fmt.Sprintf("spec.plan.edges[%d]", idx))
		if err != nil {
			return nil, err
		}
		if _, ok := ids[from]; !ok {
			return nil, genErr("edge references missing block", fmt.Sprintf("spec.plan.edges[%d].from", idx))
		}
		if _, ok := ids[to]; !ok {
			return nil, genErr("edge references missing block", fmt.Sprintf("spec.plan.edges[%d].to", idx))
		}
		on := "completed"
		if value, ok := edge["on"].(string); ok && value != "" {
			on = value
		}
		result = append(result, map[string]any{"from": from, "to": to, "on": on})
	}
	return result, nil
}

func customCycles(plan map[string]any, ids map[string]struct{}, jobs []map[string]any) ([]map[string]any, error) {
	raw, err := objectList(defaultAny(plan["cycles"], []any{}), "spec.plan.cycles")
	if err != nil {
		return nil, err
	}
	reviewIDs := map[string]struct{}{}
	for _, job := range jobs {
		if job["type"] == "review" {
			reviewIDs[fmt.Sprint(job["id"])] = struct{}{}
		}
	}
	result := []map[string]any{}
	for idx, cycle := range raw {
		from, err := requiredString(cycle, "from", fmt.Sprintf("spec.plan.cycles[%d]", idx))
		if err != nil {
			return nil, err
		}
		to, err := requiredString(cycle, "to", fmt.Sprintf("spec.plan.cycles[%d]", idx))
		if err != nil {
			return nil, err
		}
		if _, ok := ids[from]; !ok {
			return nil, genErr("cycle references missing block", fmt.Sprintf("spec.plan.cycles[%d].from", idx))
		}
		if _, ok := ids[to]; !ok {
			return nil, genErr("cycle references missing block", fmt.Sprintf("spec.plan.cycles[%d].to", idx))
		}
		if _, ok := reviewIDs[from]; !ok {
			return nil, genErr("cycle source must be a review block", fmt.Sprintf("spec.plan.cycles[%d].from", idx))
		}
		maxIterations, ok := intFrom(cycle["max_iterations"])
		if !ok || maxIterations < 1 {
			return nil, genErr("cycle max_iterations must be positive", fmt.Sprintf("spec.plan.cycles[%d].max_iterations", idx))
		}
		onVerdict := "needs_revision"
		if value, ok := cycle["on_verdict"].(string); ok && value != "" {
			onVerdict = value
		}
		result = append(result, map[string]any{"from": from, "to": to, "on_verdict": onVerdict, "max_iterations": maxIterations})
	}
	return result, nil
}

func safeArtifactPath(artifactPath, root, fieldPath string) error {
	safe, err := SafeRelativePath(artifactPath, fieldPath)
	if err != nil {
		return err
	}
	rootSafe, err := SafeRelativePath(root, fieldPath)
	if err != nil {
		return err
	}
	prefix := strings.TrimSuffix(rootSafe, "/") + "/"
	if safe != rootSafe && !strings.HasPrefix(safe, prefix) {
		return genErr("derived artifact path escapes artifact root", fieldPath)
	}
	return nil
}

func assertAcyclic(ids map[string]struct{}, edges []map[string]any) error {
	incoming := map[string]int{}
	outgoing := map[string][]string{}
	for id := range ids {
		incoming[id] = 0
	}
	for _, edge := range edges {
		from := fmt.Sprint(edge["from"])
		to := fmt.Sprint(edge["to"])
		outgoing[from] = append(outgoing[from], to)
		incoming[to]++
	}
	ready := []string{}
	for id, count := range incoming {
		if count == 0 {
			ready = append(ready, id)
		}
	}
	visited := 0
	for len(ready) > 0 {
		id := ready[len(ready)-1]
		ready = ready[:len(ready)-1]
		visited++
		for _, downstream := range outgoing[id] {
			incoming[downstream]--
			if incoming[downstream] == 0 {
				ready = append(ready, downstream)
			}
		}
	}
	if visited != len(ids) {
		return genErr("custom plan base edges contain a cycle", "spec.plan.edges")
	}
	return nil
}

func graphData(jobs, edges, cycles []map[string]any) map[string]any {
	nodes := []map[string]any{}
	for _, job := range jobs {
		nodes = append(nodes, map[string]any{"id": job["id"], "label": job["title"], "type": job["type"]})
	}
	return map[string]any{"nodes": nodes, "edges": edges, "cycles": cycles}
}

func lintWorkflow(workflow map[string]any) map[string]any {
	lint, err := workflowauthoring.Lint(workflow)
	if err != nil {
		return map[string]any{
			"valid":         false,
			"errors":        []map[string]any{{"message": err.Error()}},
			"warnings":      []map[string]any{},
			"warning_count": 0,
			"coverage":      map[string]any{"level": "weak", "score": 0, "max_score": 0},
		}
	}
	return lint
}

func isReviewKind(kind string) bool {
	return kind == "review" || kind == "evidence_audit" || kind == "final_review"
}

func hasModifier(spec Spec, modifier string) bool {
	for _, item := range spec.LaneModifiers {
		if item == modifier {
			return true
		}
	}
	return false
}

func lanesFrom(value any, laneSet string) (map[string]map[string]any, error) {
	if value == nil {
		if laneSet == "local" {
			return map[string]map[string]any{}, nil
		}
		return nil, genErr("lanes are required for this lane_set", "spec.lanes")
	}
	raw, err := object(value, "spec.lanes")
	if err != nil {
		return nil, err
	}
	result := map[string]map[string]any{}
	for key, body := range raw {
		obj, err := object(body, "spec.lanes."+key)
		if err != nil {
			return nil, err
		}
		result[key] = obj
	}
	return result, nil
}

func genErr(message, fieldPath string) error {
	return &Error{Message: message, FieldPath: fieldPath}
}

func requiredString(raw map[string]any, key, prefix string) (string, error) {
	value, ok := raw[key].(string)
	if !ok || value == "" {
		return "", genErr(key+" must be a non-empty string", prefix+"."+key)
	}
	return value, nil
}

func choice(raw map[string]any, key string, choices map[string]struct{}, prefix string) (string, error) {
	value, err := requiredString(raw, key, prefix)
	if err != nil {
		return "", err
	}
	if _, ok := choices[value]; !ok {
		return "", genErr(fmt.Sprintf("%s must be one of %v", key, sortedKeys(choices)), prefix+"."+key)
	}
	return value, nil
}

func object(value any, fieldPath string) (map[string]any, error) {
	if obj, ok := value.(map[string]any); ok {
		return cloneMap(obj), nil
	}
	return nil, genErr("value must be an object", fieldPath)
}

func objectList(value any, fieldPath string) ([]map[string]any, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, genErr("value must be a list of objects", fieldPath)
	}
	result := []map[string]any{}
	for idx, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, genErr("value must be a list of objects", fmt.Sprintf("%s[%d]", fieldPath, idx))
		}
		result = append(result, cloneMap(obj))
	}
	return result, nil
}

func stringList(value any, fieldPath string) ([]string, error) {
	switch typed := value.(type) {
	case []string:
		for idx, item := range typed {
			if item == "" {
				return nil, genErr("value must be a list of non-empty strings", fmt.Sprintf("%s[%d]", fieldPath, idx))
			}
		}
		return append([]string(nil), typed...), nil
	case []any:
		result := []string{}
		for idx, item := range typed {
			text, ok := item.(string)
			if !ok || text == "" {
				return nil, genErr("value must be a list of non-empty strings", fmt.Sprintf("%s[%d]", fieldPath, idx))
			}
			result = append(result, text)
		}
		return result, nil
	default:
		return nil, genErr("value must be a list of non-empty strings", fieldPath)
	}
}

func mustString(value any) string {
	text, _ := value.(string)
	return text
}

func defaultAny(value, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}

func intFrom(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		if typed == float64(int(typed)) {
			return int(typed), true
		}
	case json.Number:
		i, err := typed.Int64()
		if err == nil {
			return int(i), true
		}
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return i, true
		}
	}
	return 0, false
}

func boolFrom(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
		if err == nil {
			return parsed, true
		}
	}
	return false, false
}

// SupportedShapes returns the sorted shape ids `workflow generate` can produce.
// The template catalog must not advertise any other shape as generatable (#111);
// the reconcile test enforces catalog ↔ generator agreement.
func SupportedShapes() []string {
	return sortedKeys(shapes)
}

// IsSupportedShape reports whether the generator can produce the given shape.
func IsSupportedShape(shape string) bool {
	_, ok := shapes[shape]
	return ok
}

// exampleOnlyShapeHint returns a clear message when the requested shape is a
// catalog template marked example-only (generatable: false) rather than a
// generated shape, pointing the operator at its example fixture instead of the
// generic "must be one of" list (#111). Empty when the shape is not a known
// example-only template.
func exampleOnlyShapeHint(rawShape string) string {
	catalog, err := workflowtemplates.Load()
	if err != nil {
		return ""
	}
	entry, err := catalog.Get(rawShape)
	if err != nil {
		return ""
	}
	generatable, ok := entry["generatable"].(bool)
	if !ok || generatable {
		return ""
	}
	msg := fmt.Sprintf("shape %q is an example-only template, not a generated shape", rawShape)
	if path, _ := entry["example_workflow_path"].(string); path != "" {
		msg += fmt.Sprintf("; copy and adapt its example workflow at %s", path)
	}
	msg += fmt.Sprintf(", or pick a generated shape: %v", SupportedShapes())
	return msg
}

func set(values ...string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return sortedKeys(seen)
}

func sortedMapKeys(values map[string]any) []string {
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func titleFromBlockID(blockID string) string {
	words := strings.Fields(strings.ReplaceAll(blockID, "_", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func cloneMap(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		result[key] = value
	}
	return result
}

func cloneNested(input map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range input {
		if obj, ok := value.(map[string]any); ok {
			result[key] = cloneMap(obj)
		} else {
			result[key] = value
		}
	}
	return result
}

func mapFrom(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func listFrom(value any) []any {
	if value == nil {
		return []any{}
	}
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return []any{}
	}
}

var titleWordRE = regexp.MustCompile(`\b\w`)

func title(value string) string {
	return titleWordRE.ReplaceAllStringFunc(value, strings.ToUpper)
}
