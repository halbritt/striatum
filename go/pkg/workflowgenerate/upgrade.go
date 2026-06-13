package workflowgenerate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/jackc/pgx/v5"
)

type RunQueryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type UpgradeOptions struct {
	Path      string
	Force     bool
	DryRun    bool
	AddPhases bool
	Apply     bool
}

func Upgrade(ctx context.Context, runner RunQueryer, repositoryID, repoRoot string, opts UpgradeOptions) (map[string]any, error) {
	if opts.Path == "" {
		return nil, &Error{Message: "workflow.upgrade requires path", FieldPath: "path"}
	}
	target, err := SafeTarget(repoRoot, opts.Path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &Error{Message: "workflow upgrade target does not exist: " + target, FieldPath: "path"}
		}
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &Error{Message: "workflow upgrade target is not a regular file: " + target, FieldPath: "path"}
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	var workflow map[string]any
	if err := json.Unmarshal(raw, &workflow); err != nil {
		return nil, &Error{Message: "workflow JSON is invalid: " + err.Error(), FieldPath: "path"}
	}
	if err := ValidateWorkflowForUpgrade(workflow); err != nil {
		return nil, err
	}
	if opts.AddPhases {
		return upgradeAddPhases(ctx, runner, repositoryID, repoRoot, target, workflow, opts.Apply)
	}

	running, err := RunningRunsForWorkflow(ctx, runner, repositoryID, repoRoot, target)
	if err != nil {
		return nil, err
	}
	if len(running) > 0 && !opts.DryRun {
		return nil, &Error{Message: "workflow upgrade refuses to mutate a workflow with non-terminal runs: " + strings.Join(running, ", "), FieldPath: "path"}
	}

	profiles, _ := workflow["harness_profiles"].(map[string]any)
	if len(profiles) == 0 {
		status := "no_changes"
		if opts.DryRun {
			status = "would_no_changes"
		}
		return map[string]any{
			"workflow_path": target,
			"status":        status,
			"changes":       []map[string]any{},
			"conflicts":     []map[string]any{},
			"note":          "workflow has no harness_profiles section; nothing to upgrade",
		}, nil
	}

	changes := []map[string]any{}
	conflicts := []map[string]any{}
	updatedProfiles := map[string]any{}
	for _, profileID := range sortedMapKeys(profiles) {
		body, ok := profiles[profileID].(map[string]any)
		if !ok {
			updatedProfiles[profileID] = profiles[profileID]
			continue
		}
		newBody := cloneMap(body)
		fragment := harnessFragmentByToolFamily(fmt.Sprint(body["tool_family"]))
		if fragment == nil {
			updatedProfiles[profileID] = newBody
			continue
		}
		native := map[string]any{}
		if rawNative, ok := newBody["native_delegation"].(map[string]any); ok {
			native = cloneMap(rawNative)
		}
		catalogInstruction := fmt.Sprint(fragment["native_delegation_instruction"])
		currentInstruction, hasInstruction := native["instruction"]
		currentText, currentIsString := currentInstruction.(string)
		switch {
		case hasInstruction && currentInstruction == catalogInstruction:
		case !hasInstruction || (currentIsString && strings.TrimSpace(currentText) == ""):
			native["instruction"] = catalogInstruction
			changes = append(changes, map[string]any{"profile_id": profileID, "field": "native_delegation.instruction", "old_value": nullableValue(currentInstruction, hasInstruction), "new_value": catalogInstruction})
		case opts.Force:
			native["instruction"] = catalogInstruction
			changes = append(changes, map[string]any{"profile_id": profileID, "field": "native_delegation.instruction", "old_value": currentInstruction, "new_value": catalogInstruction, "forced": true})
		default:
			conflicts = append(conflicts, map[string]any{"profile_id": profileID, "field": "native_delegation.instruction", "current_value": currentInstruction, "catalog_value": catalogInstruction})
		}
		if _, ok := native["mode"]; !ok {
			if catalogMode, ok := fragment["native_delegation_mode"].(string); ok && catalogMode != "" {
				native["mode"] = catalogMode
				changes = append(changes, map[string]any{"profile_id": profileID, "field": "native_delegation.mode", "old_value": nil, "new_value": catalogMode})
			}
		}
		if len(native) > 0 {
			newBody["native_delegation"] = native
		}
		updatedProfiles[profileID] = newBody
	}

	if len(conflicts) > 0 && !opts.Force {
		return map[string]any{
			"workflow_path": target,
			"status":        "refused_conflict",
			"changes":       changes,
			"conflicts":     conflicts,
			"hint":          "rerun with force to overwrite, or edit the profile to match the catalog first",
		}, nil
	}
	if len(running) > 0 && opts.DryRun {
		return map[string]any{
			"workflow_path": target,
			"status":        "would_refuse_running",
			"changes":       changes,
			"conflicts":     conflicts,
			"running_runs":  running,
		}, nil
	}
	if len(changes) == 0 {
		status := "no_changes"
		if opts.DryRun {
			status = "would_no_changes"
		}
		return map[string]any{"workflow_path": target, "status": status, "changes": []map[string]any{}, "conflicts": []map[string]any{}}, nil
	}
	if opts.DryRun {
		return map[string]any{"workflow_path": target, "status": "would_update", "changes": changes, "conflicts": []map[string]any{}}, nil
	}

	updated := cloneMap(workflow)
	updated["harness_profiles"] = updatedProfiles
	body, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, append(body, '\n'), 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"workflow_path": target, "status": "updated", "changes": changes, "conflicts": []map[string]any{}}, nil
}

func upgradeAddPhases(ctx context.Context, runner RunQueryer, repositoryID, repoRoot, target string, workflow map[string]any, apply bool) (map[string]any, error) {
	if workflow["schema_version"] != WorkflowSchemaVersion {
		return nil, &Error{Message: "workflow upgrade --add-phases only accepts striatum.workflow.v1 inputs", FieldPath: "schema_version"}
	}
	if phases := listFrom(workflow["phases"]); len(phases) > 0 {
		status := "would_no_changes"
		if apply {
			status = "no_changes"
		}
		return map[string]any{
			"workflow_path":   target,
			"status":          status,
			"mode":            "add_phases",
			"phases_added":    []map[string]any{},
			"jobs_relabelled": []map[string]any{},
			"jobs_inserted":   []map[string]any{},
			"edges_rewritten": []map[string]any{},
			"warnings":        []string{"workflow already declares phases"},
		}, nil
	}

	running, err := RunningRunsForWorkflow(ctx, runner, repositoryID, repoRoot, target)
	if err != nil {
		return nil, err
	}
	if len(running) > 0 && apply {
		return nil, &Error{Message: "workflow upgrade refuses to mutate a workflow with non-terminal runs: " + strings.Join(running, ", "), FieldPath: "path"}
	}

	upgraded, summary, err := inferPhaseUpgrade(workflow)
	if err != nil {
		return nil, err
	}
	status := "would_update"
	if apply {
		status = "updated"
	}
	if len(running) > 0 && !apply {
		status = "would_refuse_running"
		summary["running_runs"] = running
	}
	summary["workflow_path"] = target
	summary["status"] = status
	summary["mode"] = "add_phases"
	if !apply {
		summary["hint"] = "rerun with --apply to write the rewritten workflow"
		return summary, nil
	}

	body, err := json.MarshalIndent(upgraded, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(target, append(body, '\n'), 0o644); err != nil {
		return nil, err
	}
	return summary, nil
}

func inferPhaseUpgrade(workflow map[string]any) (map[string]any, map[string]any, error) {
	jobsRaw, ok := workflow["jobs"].([]any)
	if !ok {
		return nil, nil, &Error{Message: "workflow jobs must be a list", FieldPath: "jobs"}
	}
	edgesRaw, ok := workflow["edges"].([]any)
	if !ok {
		return nil, nil, &Error{Message: "workflow edges must be a list", FieldPath: "edges"}
	}

	jobs := []map[string]any{}
	for idx, item := range jobsRaw {
		body, ok := item.(map[string]any)
		if !ok {
			return nil, nil, &Error{Message: "each job must be an object", FieldPath: fmt.Sprintf("jobs[%d]", idx)}
		}
		jobs = append(jobs, cloneMap(body))
	}

	phaseForJob := map[string]string{}
	phaseKeyToID := map[string]string{}
	phaseOrder := []string{}
	jobsRelabelled := []map[string]any{}
	for _, job := range jobs {
		jobID := fmt.Sprint(job["id"])
		if jobID == "" || jobID == "<nil>" {
			continue
		}
		phaseKey := phaseKeyForJob(job)
		if _, ok := phaseKeyToID[phaseKey]; !ok {
			phaseID := uniquePhaseID(phaseKey, valuesSet(phaseKeyToID))
			phaseKeyToID[phaseKey] = phaseID
			phaseOrder = append(phaseOrder, phaseID)
		}
		phaseID := phaseKeyToID[phaseKey]
		phaseForJob[jobID] = phaseID
		if job["phase_id"] != phaseID {
			jobsRelabelled = append(jobsRelabelled, map[string]any{"job_id": jobID, "phase_id": phaseID})
		}
		job["phase_id"] = phaseID
	}

	phases := []map[string]any{}
	for _, phaseID := range phaseOrder {
		phases = append(phases, map[string]any{"id": phaseID, "name": phaseNameFromID(phaseID)})
	}
	jobByID := map[string]map[string]any{}
	for _, job := range jobs {
		if jobID, ok := job["id"].(string); ok {
			jobByID[jobID] = job
		}
	}
	incomingByJob := incomingByJob(edgesRaw)
	ordinaryJobsByPhase := map[string][]string{}
	for _, phaseID := range phaseOrder {
		ordinaryJobsByPhase[phaseID] = []string{}
	}
	synthesisByPhase := map[string]string{}
	jobsInserted := []map[string]any{}

	for _, job := range jobs {
		jobID := fmt.Sprint(job["id"])
		phaseID, ok := phaseForJob[jobID]
		if !ok {
			continue
		}
		if isSynthesisLike(job) {
			if _, ok := synthesisByPhase[phaseID]; !ok {
				synthesisByPhase[phaseID] = jobID
			}
		} else {
			ordinaryJobsByPhase[phaseID] = append(ordinaryJobsByPhase[phaseID], jobID)
		}
	}

	for _, phaseID := range phaseOrder {
		candidate, hasCandidate := synthesisByPhase[phaseID]
		ordinary := ordinaryJobsByPhase[phaseID]
		if hasCandidate {
			for _, jobID := range ordinary {
				if _, ok := incomingByJob[candidate][jobID]; !ok {
					hasCandidate = false
					break
				}
			}
		}
		if !hasCandidate {
			candidate = uniqueJobID(phaseID+"__synthesis", jobByID)
			synthesisJob := newPhaseSynthesisJob(workflow, phaseID, candidate)
			jobs = append(jobs, synthesisJob)
			jobByID[candidate] = synthesisJob
			phaseForJob[candidate] = phaseID
			synthesisByPhase[phaseID] = candidate
			jobsInserted = append(jobsInserted, map[string]any{"job_id": candidate, "type": "phase_synthesis"})
		} else {
			jobByID[candidate]["type"] = "phase_synthesis"
			jobByID[candidate]["phase_id"] = phaseID
			phaseForJob[candidate] = phaseID
			synthesisByPhase[phaseID] = candidate
		}
	}

	newEdges := []map[string]any{}
	edgesRewritten := []map[string]any{}
	seenEdges := map[[2]string]struct{}{}
	for _, item := range edgesRaw {
		edge, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fromID := fmt.Sprint(edge["from"])
		toID := fmt.Sprint(edge["to"])
		fromPhase, hasFrom := phaseForJob[fromID]
		toPhase, hasTo := phaseForJob[toID]
		if hasFrom && hasTo && fromPhase != toPhase {
			rewrittenFrom := synthesisByPhase[fromPhase]
			edgesRewritten = append(edgesRewritten, map[string]any{"from": rewrittenFrom, "to": toID, "rewritten_from": fromID})
			fromID = rewrittenFrom
		}
		appendEdge(&newEdges, seenEdges, fromID, toID)
	}
	for _, phaseID := range phaseOrder {
		synthesisID := synthesisByPhase[phaseID]
		for _, jobID := range ordinaryJobsByPhase[phaseID] {
			appendEdge(&newEdges, seenEdges, jobID, synthesisID)
		}
	}
	for idx, phaseID := range phaseOrder[:max(0, len(phaseOrder)-1)] {
		nextPhaseID := phaseOrder[idx+1]
		synthesisID := synthesisByPhase[phaseID]
		for _, jobID := range phaseEntryJobs(nextPhaseID, ordinaryJobsByPhase, phaseForJob, newEdges) {
			appendEdge(&newEdges, seenEdges, synthesisID, jobID)
		}
	}
	for _, phase := range phases {
		phaseID := fmt.Sprint(phase["id"])
		phase["synthesis_job_id"] = synthesisByPhase[phaseID]
	}

	upgraded := cloneMap(workflow)
	upgraded["schema_version"] = "striatum.workflow.v1.1"
	upgraded["phases"] = phases
	upgraded["jobs"] = jobs
	upgraded["edges"] = newEdges
	return upgraded, map[string]any{
		"phases_added":    phases,
		"jobs_relabelled": jobsRelabelled,
		"jobs_inserted":   jobsInserted,
		"edges_rewritten": edgesRewritten,
		"warnings":        []string{},
	}, nil
}

func phaseKeyForJob(job map[string]any) string {
	if group, ok := job["parallel_group"].(string); ok && strings.TrimSpace(group) != "" {
		return splitPhaseKey(group)
	}
	jobID := fmt.Sprint(job["id"])
	if jobID == "" || jobID == "<nil>" {
		jobID = "main"
	}
	return splitPhaseKey(jobID)
}

var phaseKeySplitRE = regexp.MustCompile(`[_:-]`)
var slugRE = regexp.MustCompile(`[^a-zA-Z0-9]+`)
var numericPhasePrefixRE = regexp.MustCompile(`^\d+_`)

func splitPhaseKey(value string) string {
	parts := phaseKeySplitRE.Split(strings.TrimSpace(value), 2)
	if len(parts) == 0 || parts[0] == "" {
		return "main"
	}
	return parts[0]
}

func uniquePhaseID(phaseKey string, existing map[string]struct{}) string {
	base := slug(phaseKey)
	phaseID := base
	if !strings.HasPrefix(phaseID, "phase_") {
		phaseID = "phase_" + phaseID
	}
	candidate := phaseID
	for suffix := 2; ; suffix++ {
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", phaseID, suffix)
	}
}

func phaseNameFromID(phaseID string) string {
	name := phaseID
	name = strings.TrimPrefix(name, "phase_")
	name = numericPhasePrefixRE.ReplaceAllString(name, "")
	name = strings.TrimSpace(strings.ReplaceAll(name, "_", " "))
	if name == "" {
		return phaseID
	}
	return title(name)
}

func slug(value string) string {
	result := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "_"), "_")
	if result == "" {
		return "main"
	}
	return result
}

func incomingByJob(edges []any) map[string]map[string]struct{} {
	incoming := map[string]map[string]struct{}{}
	for _, item := range edges {
		edge, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fromID, fromOK := edge["from"].(string)
		toID, toOK := edge["to"].(string)
		if !fromOK || !toOK {
			continue
		}
		if incoming[toID] == nil {
			incoming[toID] = map[string]struct{}{}
		}
		incoming[toID][fromID] = struct{}{}
	}
	return incoming
}

func isSynthesisLike(job map[string]any) bool {
	if job["type"] == "phase_synthesis" {
		return true
	}
	text := strings.ToLower(strings.Join([]string{fmt.Sprint(job["id"]), fmt.Sprint(job["type"]), fmt.Sprint(job["title"])}, " "))
	return strings.Contains(text, "synthesis") || strings.Contains(text, "synthesize") || strings.Contains(text, "consolidate")
}

func newPhaseSynthesisJob(workflow map[string]any, phaseID, jobID string) map[string]any {
	roleID := firstMatchingKey(workflow["roles"], "review")
	if roleID == "" {
		roleID = firstKey(workflow["roles"])
	}
	if roleID == "" {
		roleID = "reviewer"
	}
	laneID := firstMatchingKey(workflow["lanes"], "review")
	if laneID == "" {
		laneID = firstKey(workflow["lanes"])
	}
	job := map[string]any{
		"id":          jobID,
		"type":        "phase_synthesis",
		"title":       phaseNameFromID(phaseID) + " Synthesis",
		"role_id":     roleID,
		"objective":   "Synthesize the phase outputs and record a phase verdict.",
		"task_prompt": map[string]any{"inline": "Review the phase outputs and publish the synthesis artifact."},
		"write_scope": map[string]any{
			"mode":            "repo_write",
			"repo_write":      true,
			"allowed_paths":   []string{"striatum/" + phaseID + "/"},
			"forbidden_paths": []string{".striatum/"},
		},
		"expected_artifacts": []map[string]any{{
			"logical_name": "synthesis",
			"kind":         "synthesis",
			"path":         "striatum/" + phaseID + "/SYNTHESIS.md",
			"required":     true,
			"placement":    artifactcontracts.PlacementGitPublication,
		}},
		"phase_id": phaseID,
	}
	if laneID != "" {
		job["lane_id"] = laneID
	}
	return job
}

func firstMatchingKey(value any, needle string) string {
	for _, key := range sortedMapKeys(mapFrom(value)) {
		if strings.Contains(strings.ToLower(key), needle) {
			return key
		}
	}
	return ""
}

func firstKey(value any) string {
	keys := sortedMapKeys(mapFrom(value))
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func uniqueJobID(base string, jobs map[string]map[string]any) string {
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, ok := jobs[candidate]; !ok {
			return candidate
		}
		candidate = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func appendEdge(edges *[]map[string]any, seen map[[2]string]struct{}, fromID, toID string) {
	if fromID == "" || fromID == "<nil>" || toID == "" || toID == "<nil>" || fromID == toID {
		return
	}
	key := [2]string{fromID, toID}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*edges = append(*edges, map[string]any{"from": fromID, "to": toID, "on": "completed"})
}

func phaseEntryJobs(phaseID string, ordinaryJobsByPhase map[string][]string, phaseForJob map[string]string, edges []map[string]any) []string {
	ordinary := ordinaryJobsByPhase[phaseID]
	incomingSamePhase := map[string]struct{}{}
	for _, edge := range edges {
		fromID := fmt.Sprint(edge["from"])
		toID := fmt.Sprint(edge["to"])
		if phaseForJob[fromID] == phaseID && phaseForJob[toID] == phaseID {
			incomingSamePhase[toID] = struct{}{}
		}
	}
	entries := []string{}
	for _, jobID := range ordinary {
		if _, ok := incomingSamePhase[jobID]; !ok {
			entries = append(entries, jobID)
		}
	}
	if len(entries) == 0 && len(ordinary) > 0 {
		return ordinary[:1]
	}
	return entries
}

func valuesSet(values map[string]string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func ValidateWorkflowForUpgrade(workflow map[string]any) error {
	if workflow["schema_version"] == nil {
		return &Error{Message: "workflow config must declare schema_version", FieldPath: "schema_version"}
	}
	return nil
}

func RunningRunsForWorkflow(ctx context.Context, runner RunQueryer, repositoryID, repoRoot, workflowPath string) ([]string, error) {
	if runner == nil {
		return nil, &Error{Message: "workflow upgrade cannot verify non-terminal runs because daemon PostgreSQL query access is unavailable", FieldPath: "path"}
	}
	candidates := workflowSourcePathCandidates(repoRoot, workflowPath)
	rows, err := runner.Query(ctx, `
		SELECT r.run_id
		  FROM striatumd.runs r
		  JOIN striatumd.workflow_snapshots w
		    ON w.repository_id = r.repository_id
		   AND w.workflow_snapshot_id = r.workflow_snapshot_id
		 WHERE r.repository_id = $1
		   AND r.state NOT IN ('completed', 'failed', 'canceled')
		   AND w.source_path = ANY($2)
		 ORDER BY r.run_id`, repositoryID, candidates)
	if err != nil {
		return nil, &Error{Message: "workflow upgrade cannot verify non-terminal runs using daemon PostgreSQL: " + err.Error(), FieldPath: "path"}
	}
	defer rows.Close()
	result := []string{}
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return nil, err
		}
		result = append(result, runID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func workflowSourcePathCandidates(repoRoot, workflowPath string) []string {
	target, _ := filepath.Abs(workflowPath)
	candidates := map[string]struct{}{target: {}}
	repoAbs, err := filepath.Abs(repoRoot)
	if err == nil {
		if rel, err := filepath.Rel(repoAbs, target); err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			candidates[filepath.ToSlash(rel)] = struct{}{}
		}
	}
	out := []string{}
	for candidate := range candidates {
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out
}

func nullableValue(value any, exists bool) any {
	if !exists {
		return nil
	}
	return value
}
