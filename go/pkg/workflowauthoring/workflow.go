package workflowauthoring

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

const (
	SchemaV1   = "striatum.workflow.v1"
	SchemaV11  = "striatum.workflow.v1.1"
	defaultFmt = "mermaid"
)

var constraintValues = map[string]map[string]bool{
	"network":     {"allowed": true, "forbidden": true, "advisory_forbidden": true},
	"transcripts": {"off": true, "redacted": true, "allowed": true},
	"repo_scope":  {"local_only": true, "unrestricted": true},
}

var enforcementLevels = map[string]int{"unsupported": 0, "advisory": 1, "advisory_strict": 2, "enforced": 3}
var worktreeIsolationValues = map[string]bool{"off": true, "per_job": true}
var reviewerAccessScopes = map[string]bool{"document_only": true, "artifact_augmented": true, "repo_level": true, "cross_repo_artifact_augmented": true}
var reviewerContextPolicies = map[string]bool{"fresh": true, "cross_round": true}
var sharedResourceModes = map[string]bool{"exclusive": true, "per_lane_namespace": true}
var reviewPostures = map[string]bool{
	"neutral": true, "devils_advocate": true, "security": true, "threat_model": true,
	"latency_performance": true, "ergonomics_dx": true, "accessibility": true,
	"compliance_license": true, "supply_chain": true,
}

var requiredTopLevel = []string{
	"schema_version",
	"workflow_id",
	"workflow_version",
	"name",
	"branch",
	"coordinator",
	"lanes",
	"roles",
	"context_docs",
	"parallelism",
	"jobs",
	"edges",
	"cycles",
}

var allowedArtifactKinds = artifactcontracts.AllowedKindSet()

var verdictJobTypes = map[string]bool{"review": true, "phase_synthesis": true}

type Error struct {
	Message   string
	FieldPath string
}

func (e *Error) Error() string { return e.Message }

func errf(format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...)}
}

func fieldErr(fieldPath string, format string, args ...any) *Error {
	return &Error{Message: fmt.Sprintf(format, args...), FieldPath: fieldPath}
}

func ResolveWorkflowPath(repoRoot string, workflowPath string) (string, string, error) {
	if strings.TrimSpace(workflowPath) == "" {
		return "", "", &Error{Message: "workflow_path must be a non-empty string", FieldPath: "workflow_path"}
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", "", err
	}
	root = filepath.Clean(root)
	if realRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = realRoot
	}
	candidate := workflowPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", "", err
	}
	candidate = filepath.Clean(candidate)
	if !pathWithin(candidate, root) {
		return "", "", &Error{Message: "workflow path must stay inside the repository"}
	}
	if realCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
		candidate = realCandidate
		if !pathWithin(candidate, root) {
			return "", "", &Error{Message: "workflow path must stay inside the repository"}
		}
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return "", "", &Error{Message: "workflow path must stay inside the repository"}
	}
	return candidate, filepath.ToSlash(rel), nil
}

func LoadFile(repoRoot string, workflowPath string) (map[string]any, string, error) {
	path, sourcePath, err := ResolveWorkflowPath(repoRoot, workflowPath)
	if err != nil {
		return nil, "", err
	}
	workflow, err := Load(path)
	if err != nil {
		return nil, "", err
	}
	return workflow, sourcePath, nil
}

func Load(path string) (map[string]any, error) {
	suffix := strings.ToLower(filepath.Ext(path))
	if suffix == ".yaml" || suffix == ".yml" {
		return nil, &Error{Message: "workflow config must be JSON, not YAML"}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Message: "read workflow: " + err.Error()}
	}
	// Detect a non-JSON file up front (GH #99): a tracked workflow.json that
	// actually holds Markdown or other prose should report a clear "not valid
	// JSON" diagnostic naming the path, distinct from schema-validation errors.
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, &Error{Message: fmt.Sprintf("workflow file is not valid JSON: %s: expected a JSON object", path)}
	}
	// GH #114: detect duplicate top-level keys before decoding. Go's
	// encoding/json silently takes last-wins for duplicate keys, so we scan
	// the token stream ourselves. Only the top-level object is checked because
	// the authoring contract does not allow duplicate sub-keys either, but the
	// duplicate-lanes bug that motivated #114 is a top-level problem.
	if dupKey, err := detectDuplicateTopLevelKey(raw); err != nil {
		return nil, &Error{Message: fmt.Sprintf("workflow file is not valid JSON: %s: %s", path, err.Error())}
	} else if dupKey != "" {
		return nil, &Error{Message: fmt.Sprintf("workflow file has duplicate top-level key %q: %s", dupKey, path)}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var workflow map[string]any
	if err := dec.Decode(&workflow); err != nil {
		return nil, &Error{Message: fmt.Sprintf("workflow file is not valid JSON: %s: %s", path, jsonErrorMessage(err))}
	}
	if workflow == nil {
		return nil, &Error{Message: fmt.Sprintf("workflow file is not valid JSON: %s: expected a JSON object", path)}
	}
	if err := Validate(workflow); err != nil {
		return nil, err
	}
	return workflow, nil
}

// detectDuplicateTopLevelKey scans the JSON token stream and returns the first
// duplicate key in the top-level object, or an empty string if none are found.
// It returns an error only when the raw bytes are not parseable JSON at all.
func detectDuplicateTopLevelKey(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	// Consume the opening '{'.
	tok, err := dec.Token()
	if err != nil {
		return "", err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return "", nil // not an object; structural validation happens elsewhere
	}
	seen := map[string]bool{}
	depth := 0 // nesting depth inside the top-level object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		if depth == 0 {
			// At depth 0 we alternate key / value. Keys are always strings.
			key, ok := tok.(string)
			if !ok {
				continue
			}
			if seen[key] {
				return key, nil
			}
			seen[key] = true
			// Skip the value: if it is a nested object or array we must
			// consume its tokens so the decoder stays in sync.
			if err := skipValue(dec, &depth); err != nil {
				return "", err
			}
		}
	}
	return "", nil
}

// skipValue advances the decoder past exactly one JSON value. It handles
// nested objects and arrays by tracking depth so the caller can stay in sync.
func skipValue(dec *json.Decoder, depth *int) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{', '[':
			*depth++
			for dec.More() {
				if err := skipValue(dec, depth); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil { // consume closing } or ]
				return err
			}
			*depth--
		}
	}
	return nil
}

func Validate(workflow map[string]any) error {
	missing := []string{}
	for _, key := range requiredTopLevel {
		if _, ok := workflow[key]; !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return errf("workflow is missing required fields: %s", strings.Join(missing, ", "))
	}
	schema, _ := workflow["schema_version"].(string)
	if schema != SchemaV1 && schema != SchemaV11 {
		return fieldErr("schema_version", "workflow schema_version must be one of: %s, %s", SchemaV1, SchemaV11)
	}
	if raw, exists := workflow["operator_content_neutrality_override_rationale"]; exists {
		text, ok := raw.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fieldErr("operator_content_neutrality_override_rationale", "operator_content_neutrality_override_rationale must be a non-empty string when set")
		}
	}
	if _, err := object(workflow, "branch"); err != nil {
		return err
	}
	lanes, err := object(workflow, "lanes")
	if err != nil {
		return err
	}
	if err := validateLanes(lanes); err != nil {
		return err
	}
	roles, err := object(workflow, "roles")
	if err != nil {
		return err
	}
	_, crossRepo := workflow["repositories"]
	if _, err := list(workflow, "context_docs"); err != nil {
		return err
	}
	if _, err := object(workflow, "parallelism"); err != nil {
		return err
	}
	jobs, err := list(workflow, "jobs")
	if err != nil {
		return err
	}
	if _, err := list(workflow, "edges"); err != nil {
		return err
	}
	if _, err := list(workflow, "cycles"); err != nil {
		return err
	}
	jobMap := map[string]map[string]any{}
	for index, item := range jobs {
		job, ok := item.(map[string]any)
		if !ok {
			return fieldErr(fmt.Sprintf("jobs[%d]", index), "each job must be an object")
		}
		jobID, err := stringField(job, "id")
		if err != nil {
			return err
		}
		if _, exists := jobMap[jobID]; exists {
			return fieldErr(fmt.Sprintf("jobs[%d].id", index), "duplicate job id %q", jobID)
		}
		jobMap[jobID] = job
		roleID, err := stringField(job, "role_id")
		if err != nil {
			return err
		}
		if _, ok := roles[roleID]; !ok {
			return fieldErr(fmt.Sprintf("jobs[%d].role_id", index), "job %q references unknown role %q", jobID, roleID)
		}
		if laneID, ok := job["lane_id"].(string); ok && laneID != "" {
			if _, ok := lanes[laneID]; !ok {
				return fieldErr(fmt.Sprintf("jobs[%d].lane_id", index), "job %q references unknown lane %q", jobID, laneID)
			}
		}
		if err := validateJobPaths(index, jobID, job); err != nil {
			return err
		}
		if err := validateReviewerPolicy(jobID, job, crossRepo); err != nil {
			return err
		}
		if err := validateReviewPosture(jobID, job); err != nil {
			return err
		}
		if err := validateRequiredReviewPostures(jobID, job); err != nil {
			return err
		}
		if err := validateSharedResources(index, jobID, job); err != nil {
			return err
		}
	}
	if err := validateEdges(workflow, jobMap); err != nil {
		return err
	}
	if err := validateInterrogationTargets(workflow, jobMap); err != nil {
		return err
	}
	// Phase-shape rules are shared with run.prepare so `workflow validate`
	// rejects the same shapes locally instead of passing then failing at
	// launch (GH #66). See pkg/workflowauthoring/phases.go.
	if err := ValidatePhaseShapes(workflow); err != nil {
		return err
	}
	if err := validateCycles(workflow, jobMap); err != nil {
		return err
	}
	if err := validateArtifactUniqueness(jobs); err != nil {
		return err
	}
	if err := validateCycleTargets(workflow, jobMap); err != nil {
		return err
	}
	if err := validateParallelism(jobs); err != nil {
		return err
	}
	if err := validateRequiredPosturesReachable(workflow, jobMap); err != nil {
		return err
	}
	if err := validateRevisionPolicy(workflow, jobs); err != nil {
		return err
	}
	return nil
}

func validateJobPaths(jobIndex int, jobID string, job map[string]any) error {
	if scope, ok := job["write_scope"].(map[string]any); ok {
		allowed := stringsFromSlice(scope["allowed_paths"])
		forbidden := stringsFromSlice(scope["forbidden_paths"])
		for _, allowedPath := range allowed {
			if repoPathInvalid(allowedPath) {
				return errf("job %q has invalid write_scope allowed_path", jobID)
			}
			for _, forbiddenPath := range forbidden {
				if repoPathInvalid(forbiddenPath) {
					return errf("job %q has invalid write_scope forbidden_path", jobID)
				}
				if repoPathWithin(allowedPath, forbiddenPath) {
					return errf("job %q write_scope allowed_path %q is inside forbidden_path %q", jobID, allowedPath, forbiddenPath)
				}
			}
		}
	}
	for artifactIndex, item := range anySlice(job["expected_artifacts"]) {
		artifact, ok := item.(map[string]any)
		if !ok {
			return errf("job %q expected artifact must be an object", jobID)
		}
		path := stringValue(artifact["path"])
		if path == "" || repoPathInvalid(path) {
			return fieldErr(fmt.Sprintf("jobs[%d].expected_artifacts[%d].path", jobIndex, artifactIndex), "job %q has invalid artifact path", jobID)
		}
		// GH #119: validate artifact kind. An empty or absent kind silently
		// passes Validate but fails at publish time with a confusing error.
		// Require a non-empty known kind so the misconfiguration surfaces at
		// author time instead of at claim/publish time.
		kind := stringValue(artifact["kind"])
		if kind == "" {
			return fieldErr(
				fmt.Sprintf("jobs[%d].expected_artifacts[%d].kind", jobIndex, artifactIndex),
				"job %q expected_artifacts[%d] is missing required field \"kind\"",
				jobID, artifactIndex,
			)
		}
		if !allowedArtifactKinds[kind] {
			return fieldErr(
				fmt.Sprintf("jobs[%d].expected_artifacts[%d].kind", jobIndex, artifactIndex),
				"job %q declares unknown artifact kind %q; valid kinds: %s",
				jobID, kind, strings.Join(sortedKindList(), ", "),
			)
		}
		if placement := strings.TrimSpace(stringValue(artifact["placement"])); placement != "" && !artifactcontracts.IsAllowedPlacement(placement) {
			return fieldErr(
				fmt.Sprintf("jobs[%d].expected_artifacts[%d].placement", jobIndex, artifactIndex),
				"job %q declares unknown artifact placement %q; valid placements: %s",
				jobID, placement, strings.Join(artifactcontracts.AllowedPlacementList(), ", "),
			)
		}
		if err := validateArtifactInWriteScope(jobID, job, path); err != nil {
			return err
		}
	}
	return nil
}

func validateLanes(lanes map[string]any) error {
	for laneID, laneVal := range lanes {
		lane, ok := laneVal.(map[string]any)
		if !ok {
			return fieldErr(fmt.Sprintf("lanes.%s", laneID), "lane %q must be an object", laneID)
		}
		if modelVal, exists := lane["model"]; exists {
			modelStr, ok := modelVal.(string)
			if !ok || modelStr == "" {
				return fieldErr(fmt.Sprintf("lanes.%s.model", laneID), "lane %q model must be a non-empty string", laneID)
			}
		}
		if lane["adapter"] == "process" {
			command := anySlice(lane["command"])
			if len(command) == 0 {
				return errf("process lane %q command must be a non-empty array", laneID)
			}
			for _, part := range command {
				if text, ok := part.(string); !ok || text == "" {
					return errf("process lane %q command entries must be non-empty strings", laneID)
				}
			}
		}
		if mode := stringValue(lane["worktree_isolation"]); mode != "" && !worktreeIsolationValues[mode] {
			return errf("lane %q worktree_isolation must be one of [off per_job]", laneID)
		}
		if raw, exists := lane["allow_shared_checkout_repo_write"]; exists {
			allow, ok := raw.(bool)
			if !ok {
				return fieldErr(fmt.Sprintf("lanes.%s.allow_shared_checkout_repo_write", laneID), "lane %q allow_shared_checkout_repo_write must be a boolean", laneID)
			}
			if allow && strings.TrimSpace(stringValue(lane["shared_checkout_repo_write_rationale"])) == "" {
				return fieldErr(fmt.Sprintf("lanes.%s.shared_checkout_repo_write_rationale", laneID), "lane %q allow_shared_checkout_repo_write requires a non-empty shared_checkout_repo_write_rationale", laneID)
			}
		}
		if raw, exists := lane["shared_checkout_repo_write_rationale"]; exists {
			text, ok := raw.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return fieldErr(fmt.Sprintf("lanes.%s.shared_checkout_repo_write_rationale", laneID), "lane %q shared_checkout_repo_write_rationale must be a non-empty string when set", laneID)
			}
		}
		// #223: first-class lane launch env. path_prefix is an array of absolute
		// directories prepended to the lane PATH; command_env is an object of
		// string env values. command_env must not set PATH (use path_prefix) or any
		// STRIATUM_-namespaced control var, so the snapshot stays portable and
		// cannot override the daemon control plane.
		if raw, exists := lane["path_prefix"]; exists {
			items, ok := raw.([]any)
			if !ok {
				return errf("lane %q path_prefix must be an array of absolute directory strings", laneID)
			}
			for _, item := range items {
				dir, ok := item.(string)
				if !ok || strings.TrimSpace(dir) == "" {
					return errf("lane %q path_prefix entries must be non-empty strings", laneID)
				}
				if !filepath.IsAbs(strings.TrimSpace(dir)) {
					return errf("lane %q path_prefix entries must be absolute paths: %s", laneID, dir)
				}
			}
		}
		if raw, exists := lane["command_env"]; exists {
			obj, ok := raw.(map[string]any)
			if !ok {
				return errf("lane %q command_env must be an object of string values", laneID)
			}
			for key, value := range obj {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					return errf("lane %q command_env keys must be non-empty", laneID)
				}
				if trimmed == "PATH" {
					return errf("lane %q command_env must not set PATH; use path_prefix instead", laneID)
				}
				if strings.HasPrefix(trimmed, "STRIATUM_") {
					return errf("lane %q command_env must not set STRIATUM_-namespaced control vars: %s", laneID, trimmed)
				}
				if _, ok := value.(string); !ok {
					return errf("lane %q command_env value for %s must be a string", laneID, trimmed)
				}
			}
		}
		// GH #119: if a lane's supervision block explicitly requests an
		// agent-loop transport (transport: "pty_helper" or require_tmux: true),
		// it must also declare adapter_capabilities.agent_loop: true (or the
		// deprecated top-level agent_loop: true). Without that flag the daemon's
		// laneUsesAgentLoop returns false: the PTY session is launched without
		// the agent-loop executor wrapper, the bootstrap prompt is never
		// delivered, and the lane silently stalls without claiming work.
		if supRaw, exists := lane["supervision"]; exists {
			sup, _ := supRaw.(map[string]any)
			if sup != nil {
				wantsAgentLoopTransport := sup["require_tmux"] == true || sup["transport"] == "pty_helper"
				if wantsAgentLoopTransport && !laneDeclaresAgentLoop(lane) {
					return fieldErr(
						fmt.Sprintf("lanes.%s.adapter_capabilities", laneID),
						"lane %q sets supervision.require_tmux/transport=pty_helper but does not declare adapter_capabilities.agent_loop: true; the PTY bootstrap prompt will not be delivered and the lane will stall",
						laneID,
					)
				}
			}
		}
		constraints := map[string]any{}
		if raw, exists := lane["constraints"]; exists {
			var ok bool
			constraints, ok = raw.(map[string]any)
			if !ok {
				return errf("lane %q constraints must be an object", laneID)
			}
			for key, value := range constraints {
				allowed, ok := constraintValues[key]
				if !ok {
					return errf("lane %q has unknown constraint %q", laneID, key)
				}
				if !allowed[fmt.Sprint(value)] {
					return errf("lane %q has invalid %q constraint value", laneID, key)
				}
			}
		}
		required, exists := lane["required_enforcement"]
		if !exists {
			continue
		}
		requiredMap, ok := required.(map[string]any)
		if !ok {
			return errf("lane %q required_enforcement must be an object", laneID)
		}
		for key, value := range requiredMap {
			if _, ok := constraints[key]; !ok {
				return errf("lane %q requires enforcement for undeclared constraint %q", laneID, key)
			}
			requiredText, ok := value.(string)
			if !ok || !enforcementLevelKnown(requiredText) {
				return errf("lane %q has invalid required enforcement level", laneID)
			}
			actual := adapterConstraintEnforcement(lane["adapter"], key, fmt.Sprint(constraints[key]))
			if !adapterEnforcementSatisfies(actual, requiredText) {
				return errf("lane %q requires %q enforcement for %q, but adapter provides %q", laneID, requiredText, key, actual)
			}
		}
	}
	return nil
}

func validateReviewerPolicy(jobID string, job map[string]any, crossRepo bool) error {
	_, hasAccess := job["reviewer_access_scope"]
	_, hasContext := job["reviewer_context_policy"]
	if !hasAccess && !hasContext {
		return nil
	}
	if defaultString(job["type"], "generic") != "review" {
		return errf("non-review job %q cannot declare reviewer_access_scope/reviewer_context_policy", jobID)
	}
	if hasAccess {
		access := stringValue(job["reviewer_access_scope"])
		if access == "" || !reviewerAccessScopes[access] {
			return errf("review job %q has unknown reviewer_access_scope %q; allowed: document_only|artifact_augmented|repo_level|cross_repo_artifact_augmented", jobID, job["reviewer_access_scope"])
		}
		if access == "cross_repo_artifact_augmented" && !crossRepo {
			return errf("review job %q may use reviewer_access_scope cross_repo_artifact_augmented only in cross-repo workflows", jobID)
		}
		// document_only reviewers read ONLY the listed input documents, so a
		// null/empty inputs list leaves them with no valid document set (GH
		// #97). The packet derives review_policy.access_scope from this field.
		if access == "document_only" && len(anySlice(job["inputs"])) == 0 {
			return errf("review job %q: review_policy.access_scope \"document_only\" requires a non-empty inputs list", jobID)
		}
	}
	if hasContext {
		context := stringValue(job["reviewer_context_policy"])
		if context == "" || !reviewerContextPolicies[context] {
			return errf("review job %q has unknown reviewer_context_policy %q; allowed: fresh|cross_round", jobID, job["reviewer_context_policy"])
		}
		if context == "fresh" && job["fresh_session_required"] == false {
			return errf("review job %q declares reviewer_context_policy=fresh but fresh_session_required=false", jobID)
		}
	}
	return nil
}

func validateReviewPosture(jobID string, job map[string]any) error {
	value, exists := job["review_posture"]
	if !exists {
		return nil
	}
	if defaultString(job["type"], "generic") != "review" {
		return errf("non-review job %q cannot declare review_posture", jobID)
	}
	posture, ok := value.(string)
	if !ok || posture == "" {
		return errf("review job %q review_posture must be a non-empty string", jobID)
	}
	return validatePostureValue(jobID, "review job", posture)
}

func validateRequiredReviewPostures(jobID string, job map[string]any) error {
	raw, exists := job["required_review_postures"]
	if !exists {
		return nil
	}
	if defaultString(job["type"], "generic") != "build" {
		return errf("non-build job %q cannot declare required_review_postures", jobID)
	}
	values := anySlice(raw)
	if len(values) == 0 {
		return errf("build job %q required_review_postures must be a non-empty list", jobID)
	}
	for _, item := range values {
		posture, ok := item.(string)
		if !ok || posture == "" {
			return errf("build job %q required_review_postures entries must be non-empty strings", jobID)
		}
		if err := validatePostureValue(jobID, "build job", posture); err != nil {
			return err
		}
	}
	return nil
}

func validateSharedResources(jobIndex int, jobID string, job map[string]any) error {
	raw, exists := job["shared_resources"]
	if !exists {
		return nil
	}
	switch raw.(type) {
	case []any, []string, []map[string]any:
	default:
		return fieldErr(fmt.Sprintf("jobs[%d].shared_resources", jobIndex), "job %q shared_resources must be a list", jobID)
	}
	items := anySlice(raw)
	for resourceIndex, item := range items {
		path := fmt.Sprintf("jobs[%d].shared_resources[%d]", jobIndex, resourceIndex)
		switch resource := item.(type) {
		case string:
			if strings.TrimSpace(resource) == "" {
				return fieldErr(path, "job %q shared_resources entries must have a non-empty id", jobID)
			}
		case map[string]any:
			id, ok := resource["id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				return fieldErr(path+".id", "job %q shared_resources entries must have a non-empty id", jobID)
			}
			mode := stringValue(resource["mode"])
			if mode == "" {
				mode = "exclusive"
			}
			if !sharedResourceModes[mode] {
				return fieldErr(path+".mode", "job %q shared_resources mode must be one of [exclusive per_lane_namespace]", jobID)
			}
			if description, exists := resource["description"]; exists {
				if _, ok := description.(string); !ok {
					return fieldErr(path+".description", "job %q shared_resources description must be a string", jobID)
				}
			}
			if namespace, exists := resource["namespace"]; exists {
				if text, ok := namespace.(string); !ok || strings.TrimSpace(text) == "" {
					return fieldErr(path+".namespace", "job %q shared_resources namespace must be a non-empty string", jobID)
				}
			}
			if mode == "per_lane_namespace" && strings.TrimSpace(stringValue(resource["namespace"])) == "" {
				return fieldErr(path+".namespace", "job %q shared_resources mode per_lane_namespace requires namespace", jobID)
			}
		default:
			return fieldErr(path, "job %q shared_resources entries must be strings or objects", jobID)
		}
	}
	return nil
}

func validatePostureValue(jobID string, label string, posture string) error {
	if reviewPostures[posture] {
		return nil
	}
	if strings.HasPrefix(posture, "custom:") && strings.TrimSpace(strings.TrimPrefix(posture, "custom:")) != "" {
		return nil
	}
	return errf("%s %q has unknown review_posture %q; allowed: first-class postures or custom:<name>", label, jobID, posture)
}

func validateArtifactInWriteScope(jobID string, job map[string]any, artifactPath string) error {
	scope, ok := job["write_scope"].(map[string]any)
	if !ok {
		return nil
	}
	allowed := stringsFromSlice(scope["allowed_paths"])
	forbidden := stringsFromSlice(scope["forbidden_paths"])
	if len(allowed) == 0 {
		return nil
	}
	for _, forbiddenPath := range forbidden {
		if repoPathWithin(artifactPath, forbiddenPath) {
			return errf("job %q expected artifact %q is inside forbidden_path %q", jobID, artifactPath, forbiddenPath)
		}
	}
	for _, allowedPath := range allowed {
		if repoPathWithin(artifactPath, allowedPath) {
			return nil
		}
	}
	return errf("job %q expected artifact %q is not inside any allowed_path", jobID, artifactPath)
}

func validateEdges(workflow map[string]any, jobMap map[string]map[string]any) error {
	seen := map[string]bool{}
	for _, item := range anySlice(workflow["edges"]) {
		edge, ok := item.(map[string]any)
		if !ok {
			return &Error{Message: "each edge must be an object"}
		}
		fromID, err := stringField(edge, "from")
		if err != nil {
			return err
		}
		toID, err := stringField(edge, "to")
		if err != nil {
			return err
		}
		if jobMap[fromID] == nil || jobMap[toID] == nil {
			return &Error{Message: "workflow edge references an unknown job"}
		}
		if edge["on"] != "completed" {
			return &Error{Message: "workflow edges must use on completed"}
		}
		seen[toID+"\x00"+fromID] = true
	}
	for jobID, job := range jobMap {
		needs, exists := job["needs"]
		if !exists {
			continue
		}
		declared := map[string]bool{}
		for _, item := range anySlice(needs) {
			dep, ok := item.(string)
			if !ok {
				return errf("job %q has non-string dependency", jobID)
			}
			declared[dep] = true
		}
		edgeNeeds := map[string]bool{}
		for key := range seen {
			parts := strings.Split(key, "\x00")
			if len(parts) == 2 && parts[0] == jobID {
				edgeNeeds[parts[1]] = true
			}
		}
		if !sameStringSet(declared, edgeNeeds) {
			return errf("job %q needs disagree with workflow edges", jobID)
		}
	}
	return nil
}

func validateInterrogationTargets(workflow map[string]any, jobMap map[string]map[string]any) error {
	edges, err := EdgeDependencyPairs(workflow)
	if err != nil {
		return err
	}
	pairs := make([][2]string, 0, len(edges))
	for _, edge := range edges {
		pairs = append(pairs, [2]string{stringValue(edge["from"]), stringValue(edge["to"])})
	}
	for jobID, job := range jobMap {
		raw, exists := job["interrogation_targets"]
		if !exists {
			continue
		}
		if job["interrogable"] == true {
			return errf("job %q cannot also declare interrogable when it declares interrogation_targets", jobID)
		}
		var targets []any
		switch typed := raw.(type) {
		case []any:
			targets = typed
		case []map[string]any:
			targets = make([]any, 0, len(typed))
			for _, item := range typed {
				targets = append(targets, item)
			}
		default:
			return errf("job %q interrogation_targets must be a list", jobID)
		}
		if len(targets) == 0 {
			return errf("job %q interrogation_targets must include at least one target", jobID)
		}
		seenTargets := map[string]bool{}
		for index, item := range targets {
			target, ok := item.(map[string]any)
			if !ok {
				return fieldErr(fmt.Sprintf("jobs.%s.interrogation_targets[%d]", jobID, index), "job %q interrogation_targets entries must be objects", jobID)
			}
			targetID, ok := target["workflow_job_id"].(string)
			if !ok || strings.TrimSpace(targetID) == "" {
				return fieldErr(fmt.Sprintf("jobs.%s.interrogation_targets[%d].workflow_job_id", jobID, index), "job %q interrogation target must declare workflow_job_id", jobID)
			}
			if targetID == jobID {
				return errf("job %q cannot target itself for interrogation", jobID)
			}
			if seenTargets[targetID] {
				return errf("job %q has duplicate interrogation target %q", jobID, targetID)
			}
			seenTargets[targetID] = true
			targetJob := jobMap[targetID]
			if targetJob == nil {
				return errf("job %q references unknown interrogation target %q", jobID, targetID)
			}
			if targetJob["interrogable"] != true {
				return errf("job %q interrogation target %q does not declare interrogable: true", jobID, targetID)
			}
			if !hasPath(pairs, targetID, jobID) {
				return errf("job %q must be reachable downstream from interrogation target %q", jobID, targetID)
			}
		}
	}
	return nil
}

func validateCycles(workflow map[string]any, jobMap map[string]map[string]any) error {
	for index, item := range anySlice(workflow["cycles"]) {
		cycle, ok := item.(map[string]any)
		if !ok {
			return &Error{Message: "each cycle must be an object"}
		}
		fromID, err := stringField(cycle, "from")
		if err != nil {
			return err
		}
		toID, err := stringField(cycle, "to")
		if err != nil {
			return err
		}
		if jobMap[fromID] == nil || jobMap[toID] == nil {
			return fieldErr(fmt.Sprintf("cycles[%d].from", index), "workflow cycle references an unknown job")
		}
		if cycle["on_verdict"] != "needs_revision" {
			return &Error{Message: "workflow cycles must use on_verdict needs_revision"}
		}
		if !positiveWholeNumber(cycle["max_iterations"]) {
			return fieldErr(fmt.Sprintf("cycles[%d].max_iterations", index), "workflow cycles must declare max_iterations >= 1")
		}
	}
	return nil
}

func validateArtifactUniqueness(jobs []any) error {
	seen := map[string]string{}
	for _, item := range jobs {
		job, ok := item.(map[string]any)
		if !ok {
			continue
		}
		jobID := stringValue(job["id"])
		for _, artifactItem := range anySlice(job["expected_artifacts"]) {
			artifact, ok := artifactItem.(map[string]any)
			if !ok {
				continue
			}
			path := normalizeRepoPath(stringValue(artifact["path"]))
			if path == "" {
				continue
			}
			if previous, exists := seen[path]; exists && previous != jobID {
				return errf("jobs %q and %q both declare expected artifact path %q", previous, jobID, path)
			}
			seen[path] = jobID
		}
	}
	return nil
}

func validateCycleTargets(workflow map[string]any, jobMap map[string]map[string]any) error {
	edges, err := EdgeDependencyPairs(workflow)
	if err != nil {
		return err
	}
	pairs := [][2]string{}
	for _, edge := range edges {
		pairs = append(pairs, [2]string{stringValue(edge["from"]), stringValue(edge["to"])})
	}
	for _, item := range anySlice(workflow["cycles"]) {
		cycle, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fromID := stringValue(cycle["from"])
		toID := stringValue(cycle["to"])
		if jobMap[fromID] == nil || jobMap[toID] == nil {
			continue
		}
		if !hasPath(pairs, toID, fromID) {
			return errf("workflow cycle from %q to %q is unsound: %q does not feed back into %q through workflow edges", fromID, toID, toID, fromID)
		}
	}
	return nil
}

func validateParallelism(jobs []any) error {
	groups := map[string][]map[string]any{}
	for _, item := range jobs {
		job, ok := item.(map[string]any)
		if !ok {
			continue
		}
		group := stringValue(job["parallel_group"])
		if group != "" {
			groups[group] = append(groups[group], job)
		}
	}
	for group, members := range groups {
		artifactPaths := map[string]bool{}
		writePaths := map[string]bool{}
		var seenRepoWrite *bool
		for _, job := range members {
			for _, artifactItem := range anySlice(job["expected_artifacts"]) {
				artifact, ok := artifactItem.(map[string]any)
				if !ok {
					continue
				}
				path := normalizeRepoPath(stringValue(artifact["path"]))
				if path == "" {
					continue
				}
				if artifactPaths[path] {
					return errf("parallel group %q reuses artifact path %q", group, path)
				}
				artifactPaths[path] = true
			}
			scope, _ := job["write_scope"].(map[string]any)
			repoWrite := scope["repo_write"] == true
			if seenRepoWrite == nil {
				value := repoWrite
				seenRepoWrite = &value
			} else if *seenRepoWrite != repoWrite {
				return errf("parallel group %q mixes repo_write and review-only jobs; split them into separate groups", group)
			}
			if !repoWrite {
				continue
			}
			for _, allowed := range stringsFromSlice(scope["allowed_paths"]) {
				path := normalizeRepoPath(allowed)
				if path == "" {
					continue
				}
				for seen := range writePaths {
					if repoPathWithin(path, seen) || repoPathWithin(seen, path) {
						return errf("parallel group %q has overlapping write scope", group)
					}
				}
				writePaths[path] = true
			}
		}
	}
	return nil
}

func validateRequiredPosturesReachable(workflow map[string]any, jobMap map[string]map[string]any) error {
	for buildID, build := range jobMap {
		if defaultString(build["type"], "generic") != "build" {
			continue
		}
		required := stringsFromSlice(build["required_review_postures"])
		if len(required) == 0 {
			continue
		}
		reachable := reachableJobs(workflow, buildID)
		available := map[string]bool{}
		for candidateID := range reachable {
			candidate := jobMap[candidateID]
			if candidate == nil || defaultString(candidate["type"], "generic") != "review" {
				continue
			}
			posture := stringValue(candidate["review_posture"])
			if posture == "" {
				posture = "neutral"
			}
			available[posture] = true
		}
		for _, posture := range required {
			if !available[posture] {
				return errf("build job %q requires review posture %q but no reachable review job declares it", buildID, posture)
			}
		}
	}
	return nil
}

func validateRevisionPolicy(workflow map[string]any, jobs []any) error {
	raw, exists := workflow["review_revision_policy"]
	if !exists {
		return nil
	}
	policy, ok := raw.(map[string]any)
	if !ok {
		return errf("review_revision_policy must be an object")
	}
	rootPolicy := stringValue(policy["root_review_needs_revision"])
	if rootPolicy != "human_checkpoint" && rootPolicy != "declared_cycle" {
		return errf("review_revision_policy.root_review_needs_revision is invalid")
	}
	if description, exists := policy["description"]; exists {
		if _, ok := description.(string); !ok {
			return errf("review_revision_policy.description must be a string")
		}
	}
	if rootPolicy != "declared_cycle" {
		return nil
	}
	cycleSources := map[string]bool{}
	for _, item := range anySlice(workflow["cycles"]) {
		cycle, ok := item.(map[string]any)
		if ok && cycle["on_verdict"] == "needs_revision" {
			cycleSources[stringValue(cycle["from"])] = true
		}
	}
	missing := []string{}
	for _, reviewID := range rootReviewJobIDs(workflow, jobs) {
		if !cycleSources[reviewID] {
			missing = append(missing, reviewID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return errf("declared_cycle review_revision_policy requires needs_revision cycles for root review jobs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func reachableJobs(workflow map[string]any, start string) map[string]bool {
	forward := map[string][]string{}
	reverse := map[string][]string{}
	for _, item := range anySlice(workflow["edges"]) {
		edge, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fromID := stringValue(edge["from"])
		toID := stringValue(edge["to"])
		forward[fromID] = append(forward[fromID], toID)
		reverse[toID] = append(reverse[toID], fromID)
	}
	result := traverse(start, forward)
	for id := range traverse(start, reverse) {
		result[id] = true
	}
	return result
}

func traverse(start string, edges map[string][]string) map[string]bool {
	seen := map[string]bool{}
	stack := []string{start}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range edges[current] {
			if seen[next] {
				continue
			}
			seen[next] = true
			stack = append(stack, next)
		}
	}
	return seen
}

func rootReviewJobIDs(workflow map[string]any, jobs []any) []string {
	targets := map[string]bool{}
	for _, item := range anySlice(workflow["edges"]) {
		edge, ok := item.(map[string]any)
		if ok {
			targets[stringValue(edge["to"])] = true
		}
	}
	result := []string{}
	for _, item := range jobs {
		job, ok := item.(map[string]any)
		if !ok || defaultString(job["type"], "generic") != "review" {
			continue
		}
		jobID := stringValue(job["id"])
		if !targets[jobID] {
			result = append(result, jobID)
		}
	}
	return result
}

func adapterConstraintEnforcement(adapter any, constraint string, requested string) string {
	if adapter == "process" {
		if constraint == "transcripts" && requested == "off" {
			return "enforced"
		}
		if constraint == "network" && requested == "forbidden" {
			return "advisory_strict"
		}
		if constraint == "repo_scope" && requested == "local_only" {
			return "advisory_strict"
		}
		return "advisory"
	}
	return "unsupported"
}

func adapterEnforcementSatisfies(actual string, required string) bool {
	return enforcementLevels[actual] >= enforcementLevels[required]
}

func enforcementLevelKnown(level string) bool {
	_, ok := enforcementLevels[level]
	return ok
}

func object(value map[string]any, key string) (map[string]any, error) {
	item, ok := value[key].(map[string]any)
	if !ok {
		return nil, errf("workflow field %q must be an object", key)
	}
	return item, nil
}

func list(value map[string]any, key string) ([]any, error) {
	switch item := value[key].(type) {
	case []any:
		return item, nil
	case []map[string]any:
		result := make([]any, 0, len(item))
		for _, entry := range item {
			result = append(result, entry)
		}
		return result, nil
	}
	return nil, errf("workflow field %q must be a list", key)
}

func stringField(value map[string]any, key string) (string, error) {
	item, ok := value[key].(string)
	if !ok || item == "" {
		return "", errf("workflow field %q must be a non-empty string", key)
	}
	return item, nil
}

func pathWithin(child string, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func repoPathInvalid(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") {
		return true
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func repoPathWithin(child string, parent string) bool {
	childNorm := normalizeRepoPath(child)
	parentNorm := normalizeRepoPath(parent)
	if parentNorm == "" || childNorm == parentNorm {
		return true
	}
	return strings.HasPrefix(childNorm, parentNorm+"/")
}

func normalizeRepoPath(path string) string {
	parts := []string{}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return strings.TrimRight(strings.Join(parts, "/"), "/")
}

func hasPath(edges [][2]string, source string, target string) bool {
	stack := []string{source}
	seen := map[string]bool{}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == target {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		for _, edge := range edges {
			if edge[0] == current {
				stack = append(stack, edge[1])
			}
		}
	}
	return false
}

func anySlice(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	if items, ok := value.([]string); ok {
		result := make([]any, 0, len(items))
		for _, item := range items {
			result = append(result, item)
		}
		return result
	}
	if items, ok := value.([]map[string]any); ok {
		result := make([]any, 0, len(items))
		for _, item := range items {
			result = append(result, item)
		}
		return result
	}
	return []any{}
}

func typedMaps(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		out := []map[string]any{}
		for _, item := range items {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return []map[string]any{}
	}
}

func stringsFromSlice(value any) []string {
	out := []string{}
	switch items := value.(type) {
	case []string:
		return append(out, items...)
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	}
	for _, item := range anySlice(value) {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func defaultString(value any, fallback string) string {
	if text := stringValue(value); text != "" {
		return text
	}
	return fallback
}

func nullableString(value any) any {
	if text := stringValue(value); text != "" {
		return text
	}
	return nil
}

func writeScopeMode(job map[string]any) any {
	if scope, ok := job["write_scope"].(map[string]any); ok {
		if mode := stringValue(scope["mode"]); mode != "" {
			return mode
		}
	}
	return nil
}

func positiveWholeNumber(value any) bool {
	switch item := value.(type) {
	case json.Number:
		integer, err := item.Int64()
		return err == nil && integer >= 1
	case int:
		return item >= 1
	case int64:
		return item >= 1
	case float64:
		return item >= 1 && item == float64(int64(item))
	default:
		return false
	}
}

func sameStringSet(left map[string]bool, right map[string]bool) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if !right[key] {
			return false
		}
	}
	return true
}

func jsonErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sortedKindList returns the allowed artifact kind names in alphabetical order,
// used to produce actionable error messages naming the full valid set.
func sortedKindList() []string {
	kinds := make([]string, 0, len(allowedArtifactKinds))
	for kind := range allowedArtifactKinds {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
