package workflowauthoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/workflowtemplates"
)

var modelTokenPattern = regexp.MustCompile(`[^a-z0-9]+`)

func WorkflowFingerprint(workflow map[string]any) (string, error) {
	normalized, err := json.Marshal(workflow)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:]), nil
}

func Lint(workflow map[string]any) (map[string]any, error) {
	if err := Validate(workflow); err != nil {
		return map[string]any{
			"workflow_id":   stringValue(workflow["workflow_id"]),
			"valid":         false,
			"errors":        []map[string]any{workflowErrorMap(err)},
			"warnings":      []map[string]any{},
			"warning_count": 0,
			"coverage":      invalidLintCoverage(),
		}, nil
	}
	jobMap := WorkflowJobMap(workflow)
	findings := []map[string]any{}
	lintSameModelReviewPairs(workflow, jobMap, &findings)
	lintOperatorContentNeutrality(workflow, jobMap, &findings)
	lintReviewFreshness(jobMap, &findings)
	lintWriteScopeRisk(workflow, jobMap, &findings)
	findings = append(findings, DaemonContractScopeWarnings(workflow)...)
	lintParallelSharedResources(jobMap, &findings)
	lintMissingEscalationPath(workflow, jobMap, &findings)
	lintAgyOneShotPipeLane(workflow, &findings)
	lintDeprecatedClaudePrintLane(workflow, &findings)
	lintDeprecatedCodexExecLane(workflow, &findings)
	lintAdapterFlagMismatches(workflow, &findings)
	lintExperimentalShape(workflow, &findings)
	lintDegradedSeatLane(workflow, &findings)
	lintInterrogationTargets(workflow, jobMap, &findings)
	lintPhaseGateSequencing(workflow, jobMap, &findings)
	for index := range findings {
		findings[index]["fingerprint"] = FindingFingerprint(findings[index])
	}
	return map[string]any{
		"workflow_id":   stringValue(workflow["workflow_id"]),
		"valid":         true,
		"errors":        []map[string]any{},
		"warnings":      findings,
		"warning_count": len(findings),
		"coverage":      lintCoverage(workflow, jobMap, findings),
	}, nil
}

func FindingFingerprint(finding map[string]any) string {
	normalized := map[string]any{}
	for key, value := range finding {
		if key == "fingerprint" {
			continue
		}
		normalized[key] = value
	}
	payload, _ := json.Marshal(map[string]any{
		"rule":    stringValue(normalized["rule"]),
		"finding": normalized,
	})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func workflowErrorMap(err error) map[string]any {
	result := map[string]any{"message": err.Error()}
	if workflowErr, ok := err.(*Error); ok && workflowErr.FieldPath != "" {
		result["field_path"] = workflowErr.FieldPath
	}
	return result
}

func lintSameModelReviewPairs(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	emitted := map[string]bool{}
	familyForJob := func(job map[string]any) string {
		return laneModelFamily(lanes, stringValue(job["lane_id"]))
	}
	edges, err := EdgeDependencyPairs(workflow)
	if err == nil {
		for _, edge := range edges {
			fromID := stringValue(edge["from"])
			toID := stringValue(edge["to"])
			upstream := jobMap[fromID]
			reviewJob := jobMap[toID]
			if defaultString(reviewJob["type"], "generic") != "review" || verdictJobTypes[defaultString(upstream["type"], "generic")] {
				continue
			}
			upstreamFamily := familyForJob(upstream)
			reviewFamily := familyForJob(reviewJob)
			if upstreamFamily == "" || reviewFamily == "" || upstreamFamily != reviewFamily {
				continue
			}
			key := "edge\x00" + fromID + "\x00" + toID
			if emitted[key] {
				continue
			}
			emitted[key] = true
			*findings = append(*findings, map[string]any{
				"rule":           "same_model_review_pair",
				"severity":       "warning",
				"message":        "review job '" + toID + "' and upstream job '" + fromID + "' use the same model family '" + reviewFamily + "'; use an independent review lane or record an explicit override rationale",
				"job_id":         toID,
				"related_job_id": fromID,
				"model_family":   reviewFamily,
			})
		}
		for _, edge := range edges {
			fromID := stringValue(edge["from"])
			toID := stringValue(edge["to"])
			upstream := jobMap[fromID]
			adjudicatorJob := jobMap[toID]
			if upstream == nil || adjudicatorJob == nil || !isCollaborationAdjudicatorJob(adjudicatorJob) {
				continue
			}
			upstreamFamily := familyForJob(upstream)
			adjudicatorFamily := familyForJob(adjudicatorJob)
			if upstreamFamily == "" || adjudicatorFamily == "" || upstreamFamily != adjudicatorFamily {
				continue
			}
			key := "adjudicator\x00" + fromID + "\x00" + toID
			if emitted[key] {
				continue
			}
			emitted[key] = true
			*findings = append(*findings, map[string]any{
				"rule":           "same_model_adjudicator_pair",
				"severity":       "warning",
				"message":        "adjudicator job '" + toID + "' and upstream job '" + fromID + "' use the same model family '" + adjudicatorFamily + "'; use an independent adjudicator lane or record an explicit override rationale",
				"job_id":         toID,
				"related_job_id": fromID,
				"model_family":   adjudicatorFamily,
			})
		}
	}
	for _, item := range anySlice(workflow["cycles"]) {
		cycle, ok := item.(map[string]any)
		if !ok || cycle["allow_same_model"] == true {
			continue
		}
		cycleFrom := stringValue(cycle["from"])
		cycleTo := stringValue(cycle["to"])
		cycleReview := jobMap[cycleFrom]
		implementer := jobMap[cycleTo]
		if cycleReview == nil || implementer == nil || !verdictJobTypes[defaultString(cycleReview["type"], "generic")] {
			continue
		}
		reviewFamily := familyForJob(cycleReview)
		implementerFamily := familyForJob(implementer)
		if reviewFamily == "" || implementerFamily == "" || reviewFamily != implementerFamily {
			continue
		}
		key := "cycle\x00" + cycleFrom + "\x00" + cycleTo
		if emitted[key] {
			continue
		}
		emitted[key] = true
		*findings = append(*findings, map[string]any{
			"rule":           "same_model_revision_cycle",
			"severity":       "warning",
			"message":        "revision cycle '" + cycleFrom + "' -> '" + cycleTo + "' returns review to the same model family '" + reviewFamily + "'; set cycle.allow_same_model=true only with an override rationale",
			"job_id":         cycleFrom,
			"related_job_id": cycleTo,
			"model_family":   reviewFamily,
		})
	}
}

func lintOperatorContentNeutrality(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	if strings.TrimSpace(stringValue(workflow["operator_content_neutrality_override_rationale"])) != "" {
		return
	}
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	coordinator, ok := workflow["coordinator"].(map[string]any)
	if !ok {
		return
	}
	coordinatorLaneID := stringValue(coordinator["lane_id"])
	coordinatorFamily := laneModelFamily(lanes, coordinatorLaneID)
	if coordinatorLaneID == "" || coordinatorFamily == "" {
		return
	}
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		if !isOperatorContentGateJob(job) {
			continue
		}
		laneID := stringValue(job["lane_id"])
		jobFamily := laneModelFamily(lanes, laneID)
		if laneID == "" || jobFamily == "" || jobFamily != coordinatorFamily {
			continue
		}
		*findings = append(*findings, map[string]any{
			"rule":                "operator_content_role_model_overlap",
			"severity":            "warning",
			"message":             "coordinator lane '" + coordinatorLaneID + "' and content gate job '" + jobID + "' use the same model family '" + jobFamily + "'; keep operator/coordinator content-neutral or record operator_content_neutrality_override_rationale",
			"job_id":              jobID,
			"lane_id":             laneID,
			"coordinator_lane_id": coordinatorLaneID,
			"model_family":        jobFamily,
		})
	}
}

func isOperatorContentGateJob(job map[string]any) bool {
	jobType := defaultString(job["type"], "generic")
	if jobType == "synthesis" || jobType == "phase_synthesis" {
		return true
	}
	if isCollaborationAdjudicatorJob(job) {
		return true
	}
	jobID := strings.ToLower(stringValue(job["id"]))
	roleID := strings.ToLower(stringValue(job["role_id"]))
	return jobType == "review" && (strings.Contains(jobID, "final_review") || strings.Contains(roleID, "final"))
}

func isCollaborationAdjudicatorJob(job map[string]any) bool {
	if stringValue(job["role_id"]) == "adjudicator" {
		return true
	}
	for _, item := range anySlice(job["expected_artifacts"]) {
		artifact, ok := item.(map[string]any)
		if ok && stringValue(artifact["kind"]) == "collaboration_ledger" {
			return true
		}
	}
	return false
}

func lintReviewFreshness(jobMap map[string]map[string]any, findings *[]map[string]any) {
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		if defaultString(job["type"], "generic") != "review" {
			continue
		}
		if effectiveFreshSessionRequired(job) || job["reviewer_context_policy"] == "fresh" {
			continue
		}
		*findings = append(*findings, map[string]any{
			"rule":     "review_without_fresh_context",
			"severity": "warning",
			"message":  "review job '" + jobID + "' does not require a fresh session; fresh review context reduces reviewer contamination",
			"job_id":   jobID,
		})
	}
}

func lintWriteScopeRisk(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	laneMap, _ := workflow["lanes"].(map[string]any)
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		if !jobIsRepoWrite(job) {
			continue
		}
		scope := job["write_scope"].(map[string]any)
		allowed := stringsFromSlice(scope["allowed_paths"])
		broad := len(allowed) == 0
		for _, item := range allowed {
			if item == "" || item == "." || item == "./" || item == "/" {
				broad = true
			}
		}
		if broad {
			*findings = append(*findings, map[string]any{
				"rule":     "broad_write_scope",
				"severity": "warning",
				"message":  "repo-write job '" + jobID + "' has broad or empty allowed_paths; narrow write scope before running untrusted changes",
				"job_id":   jobID,
			})
		}
		laneID := stringValue(job["lane_id"])
		lane, _ := laneMap[laneID].(map[string]any)
		if lane == nil || lane["worktree_isolation"] != "per_job" {
			*findings = append(*findings, map[string]any{
				"rule":     "repo_write_without_worktree_isolation",
				"severity": "warning",
				"message":  "repo-write job '" + jobID + "' is not on a per-job worktree lane; parallel or revision work can collide in the main worktree, and supervised/agent-loop repo-write lanes are refused unless they record a shared-checkout compatibility override",
				"job_id":   jobID,
			})
		}
		if lane != nil && laneIsAutonomousOrSupervised(lane) && lane["worktree_isolation"] != "per_job" && LaneAllowsSharedCheckoutRepoWrite(lane) {
			// #383 item 4: surface the concurrency consequence at validate time so the
			// gate agrees with `run start`. A shared-checkout repo-write lane passes
			// validate, but run.start refuses it (concurrent_run_isolation_required)
			// when another run is already active on the repo — the start-time gate and
			// the validate-time gate must not silently disagree. Naming it here lets the
			// author choose worktree_isolation: per_job before they hit the refusal.
			*findings = append(*findings, map[string]any{
				"rule":     "shared_checkout_repo_write_override",
				"severity": "warning",
				"message":  "repo-write job '" + jobID + "' uses supervised/agent-loop lane '" + laneID + "' in the shared checkout under allow_shared_checkout_repo_write; keep this for explicit interactive-human compatibility only. This precludes concurrent runs: run.start will refuse this run (concurrent_run_isolation_required) while another run is active on the repo. Set worktree_isolation: per_job to run concurrently.",
				"job_id":   jobID,
				"lane_id":  laneID,
			})
		}
	}
}

type sharedResourceClaim struct {
	jobID     string
	mode      string
	namespace string
}

func lintParallelSharedResources(jobMap map[string]map[string]any, findings *[]map[string]any) {
	groups := map[string][]map[string]any{}
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		group := stringValue(job["parallel_group"])
		if group == "" {
			continue
		}
		groups[group] = append(groups[group], job)
	}
	for _, group := range sortedKeys(groups) {
		claimsByResource := map[string][]sharedResourceClaim{}
		for _, job := range groups[group] {
			jobID := stringValue(job["id"])
			for _, resource := range plannedSharedResources(job) {
				id := stringValue(resource["id"])
				if id == "" {
					continue
				}
				claimsByResource[id] = append(claimsByResource[id], sharedResourceClaim{
					jobID:     jobID,
					mode:      defaultString(resource["mode"], "exclusive"),
					namespace: stringValue(resource["namespace"]),
				})
			}
		}
		for _, resourceID := range sortedKeysFromClaims(claimsByResource) {
			claims := claimsByResource[resourceID]
			if len(claims) < 2 {
				continue
			}
			if exclusiveSharedResourceJobs(claims, group, resourceID, findings) {
				continue
			}
			namespaceSharedResourceJobs(claims, group, resourceID, findings)
		}
	}
}

func exclusiveSharedResourceJobs(claims []sharedResourceClaim, group string, resourceID string, findings *[]map[string]any) bool {
	hasExclusive := false
	jobIDs := []string{}
	for _, claim := range claims {
		if claim.mode == "exclusive" {
			hasExclusive = true
		}
		jobIDs = append(jobIDs, claim.jobID)
	}
	if !hasExclusive {
		return false
	}
	sort.Strings(jobIDs)
	*findings = append(*findings, map[string]any{
		"rule":           "parallel_shared_resource_contention",
		"severity":       "warning",
		"message":        "parallel group '" + group + "' has jobs sharing exclusive resource '" + resourceID + "'; serialize those jobs or declare distinct per-lane namespaces",
		"parallel_group": group,
		"resource_id":    resourceID,
		"job_ids":        jobIDs,
	})
	return true
}

func namespaceSharedResourceJobs(claims []sharedResourceClaim, group string, resourceID string, findings *[]map[string]any) {
	byNamespace := map[string][]string{}
	for _, claim := range claims {
		if claim.mode != "per_lane_namespace" || claim.namespace == "" {
			continue
		}
		byNamespace[claim.namespace] = append(byNamespace[claim.namespace], claim.jobID)
	}
	for _, namespace := range sortedKeysFromStringSlices(byNamespace) {
		jobIDs := byNamespace[namespace]
		if len(jobIDs) < 2 {
			continue
		}
		sort.Strings(jobIDs)
		*findings = append(*findings, map[string]any{
			"rule":           "parallel_shared_resource_contention",
			"severity":       "warning",
			"message":        "parallel group '" + group + "' has jobs sharing resource '" + resourceID + "' namespace '" + namespace + "'; use distinct namespaces or serialize the jobs",
			"parallel_group": group,
			"resource_id":    resourceID,
			"namespace":      namespace,
			"job_ids":        jobIDs,
		})
	}
}

func lintMissingEscalationPath(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	hasReview := false
	for _, job := range jobMap {
		if verdictJobTypes[defaultString(job["type"], "generic")] {
			hasReview = true
			break
		}
	}
	if !hasReview {
		return
	}
	if policy, ok := workflow["review_revision_policy"].(map[string]any); ok && policy["root_review_needs_revision"] == "human_checkpoint" {
		return
	}
	for _, item := range anySlice(workflow["cycles"]) {
		if cycle, ok := item.(map[string]any); ok && cycle["on_verdict"] == "needs_revision" {
			return
		}
	}
	*findings = append(*findings, map[string]any{
		"rule":     "missing_review_escalation_path",
		"severity": "warning",
		"message":  "workflow has review jobs but no needs_revision cycle or review_revision_policy.root_review_needs_revision=human_checkpoint",
	})
}

// lintAgyOneShotPipeLane warns when an `agy` (Antigravity) lane is configured
// as a one-shot pipe lane (`agy … --print`, typically with a stdin shim or
// supervision.stdin_delivery=one_shot_eof) without declaring
// adapter_capabilities.agent_loop=true. The one-shot pipe path gives agy no
// auto-MCP config and no auto-delivery, so it launches, reads nothing on
// stdin, runs `agy --print ""` (empty), and exits without claiming (#51,
// #63 F5). agy self-claims only as an agent-loop lane. The check is
// intentionally agy-specific; retired Claude/Codex one-shot modes have their
// own hard-refusal rules.
func lintAgyOneShotPipeLane(workflow map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if laneDeclaresAgentLoop(lane) {
			continue
		}
		if !laneCommandIsAgyPrint(lane) {
			continue
		}
		*findings = append(*findings, map[string]any{
			"rule":     "agy_one_shot_pipe_lane",
			"severity": "warning",
			"message":  "lane '" + laneID + "' runs `agy --print` as a one-shot pipe lane without adapter_capabilities.agent_loop=true; agy does not self-claim on the one-shot path (no auto-MCP config, no auto-delivery). Configure agy as an agent-loop lane: command [\"agy\", \"--dangerously-skip-permissions\"] with \"adapter_capabilities\": {\"agent_loop\": true} (see #51, #63 F5)",
			"lane_id":  laneID,
		})
	}
}

// lintExperimentalShape warns (RFC 0106) when a workflow declares a generator
// shape that has no unattended-reliability gate (support_tier=experimental). It
// does NOT block — yolo may opt in knowingly — but the default run path surfaces
// the risk. A workflow with no declared `shape` (hand-authored) is not
// classified and produces no warning.
func lintExperimentalShape(workflow map[string]any, findings *[]map[string]any) {
	shape := stringValue(workflow["shape"])
	if shape == "" {
		return
	}
	if workflowtemplates.SupportTierForShape(shape) != workflowtemplates.SupportTierExperimental {
		return
	}
	*findings = append(*findings, map[string]any{
		"rule":     "experimental_shape",
		"severity": "warning",
		"message":  fmt.Sprintf("workflow shape %q is experimental: it has no unattended-reliability gate (RFC 0105), so a multi-lane / revision run may wedge without recovery — expect to supervise it, or choose a `supported` shape. See RFC 0106.", shape),
		"shape":    shape,
	})
}

// lintDegradedSeatLane warns (RFC 0109 P2) when a workflow declares a lane whose
// adapter SEAT is `degraded` or `unsupported` — a seat with a known, tracked
// defect that prevents it holding a reliable supervised multi-turn lane (today:
// `agy`, #95/#85/#76/#139). Without this, a declared 3-lane panel silently
// collapses to 2 when the agy seat trust-gates and multi-turn-crashes out (#139):
// the workflow SAYS three, the run DELIVERS two, and nothing fails. The warning
// makes that degradation a recorded, surfaced event instead.
//
// It does NOT block (yolo may opt in knowingly) and it does NOT warn on
// `experimental` seats (for example claude today: it holds a seat, just ungated
// by an installed-CLI fixture) — faithful to RFC 0109's acceptance, which
// surfaces `degraded`/`unsupported` seats specifically. The seat may only
// graduate to `supported` once its RFC 0109 P3 installed-CLI conformance fixture
// is green (#149), enforced by the adapterconformance graduation guard.
func lintDegradedSeatLane(workflow map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		adapter := laneAdapter(lane)
		if adapter == "" {
			continue
		}
		tier := workflowtemplates.SeatTierForAdapter(adapter)
		if tier != workflowtemplates.SeatTierDegraded && tier != workflowtemplates.SeatTierUnsupported {
			continue
		}
		message := fmt.Sprintf("lane %q declares the %q adapter whose supervised seat is %s: %s. A workflow that declares this lane may silently deliver one fewer voice than it names (#139) — expect to supervise it, or use a seat that holds. See RFC 0109.",
			laneID, adapter, tier, workflowtemplates.SeatDegradationReason(adapter))
		*findings = append(*findings, map[string]any{
			"rule":      "degraded_seat_lane",
			"severity":  "warning",
			"message":   message,
			"lane_id":   laneID,
			"adapter":   adapter,
			"seat_tier": tier,
		})
	}
}

func lintInterrogationTargets(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	lanes, _ := workflow["lanes"].(map[string]any)
	directDeps := map[string]bool{}
	for _, item := range anySlice(workflow["edges"]) {
		edge, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fromID := stringValue(edge["from"])
		toID := stringValue(edge["to"])
		if fromID != "" && toID != "" {
			directDeps[toID+"\x00"+fromID] = true
		}
	}
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		targets := anySlice(job["interrogation_targets"])
		if len(targets) == 0 {
			continue
		}
		if len(targets) > 3 {
			*findings = append(*findings, map[string]any{
				"rule":         "interrogation_target_count_high",
				"severity":     "warning",
				"message":      fmt.Sprintf("job %q declares %d interrogation targets; more than three targets increases operator and lane complexity", jobID, len(targets)),
				"job_id":       jobID,
				"target_count": len(targets),
			})
		}
		laneID := stringValue(job["lane_id"])
		if !laneHasCapability(lanes, laneID, "interrogate") {
			*findings = append(*findings, map[string]any{
				"rule":     "interrogation_target_missing_interrogate_capability",
				"severity": "warning",
				"message":  fmt.Sprintf("job %q declares interrogation_targets but lane %q does not declare the interrogate capability", jobID, laneID),
				"job_id":   jobID,
				"lane_id":  laneID,
			})
		}
		for index, item := range targets {
			target, ok := item.(map[string]any)
			if !ok {
				continue
			}
			targetID := stringValue(target["workflow_job_id"])
			for key := range target {
				if key == "workflow_job_id" || key == "required" {
					continue
				}
				*findings = append(*findings, map[string]any{
					"rule":          "interrogation_target_unknown_field",
					"severity":      "warning",
					"message":       fmt.Sprintf("job %q interrogation_targets[%d] has unknown field %q; V1 ignores it", jobID, index, key),
					"job_id":        jobID,
					"target_job_id": targetID,
					"field":         key,
				})
			}
			if directDeps[jobID+"\x00"+targetID] {
				*findings = append(*findings, map[string]any{
					"rule":           "interrogation_target_redundant_direct_dependency",
					"severity":       "warning",
					"message":        fmt.Sprintf("job %q declares interrogation target %q that is already a direct dependency", jobID, targetID),
					"job_id":         jobID,
					"target_job_id":  targetID,
					"related_job_id": targetID,
				})
			}
		}
	}
}

// lintPhaseGateSequencing advises on RFC 0094 §2 work-packet type sequencing.
// Hard reference errors (missing gate job, unknown withheld type, missing
// dependency edge) already fail Validate (see validatePhaseGateSequencing).
//
// This advisory surfaces the sequencing for operator visibility: an info-level
// finding naming the withheld types and the gate job per declaring phase, so a
// reviewer of `workflow lint` output sees the type-sequencing constraint without
// reading the phase block. The hard reference checks are Validate's job; this is
// pure legibility.
func lintPhaseGateSequencing(workflow map[string]any, jobMap map[string]map[string]any, findings *[]map[string]any) {
	phases := anySlice(workflow["phases"])
	if len(phases) == 0 {
		return
	}
	for _, item := range phases {
		phase, ok := item.(map[string]any)
		if !ok {
			continue
		}
		gate, ok := phase["gate"].(map[string]any)
		if !ok {
			continue
		}
		if _, declared := gate["withhold_packet_types"]; !declared {
			continue
		}
		phaseID := stringValue(phase["id"])
		gateJobID := stringValue(gate["until_verdict_clears"])
		sortedWithheld := append([]string(nil), stringsFromSlice(gate["withhold_packet_types"])...)
		sort.Strings(sortedWithheld)
		*findings = append(*findings, map[string]any{
			"rule":        "phase_gate_type_sequencing",
			"severity":    "info",
			"message":     fmt.Sprintf("phase %q withholds work-packet type(s) %s until gate job %q clears (RFC 0094 §2)", phaseID, strings.Join(sortedWithheld, ", "), gateJobID),
			"phase_id":    phaseID,
			"gate_job_id": gateJobID,
		})
	}
}

func laneHasCapability(lanes map[string]any, laneID string, capability string) bool {
	lane, ok := lanes[laneID].(map[string]any)
	if !ok {
		return false
	}
	for _, item := range anySlice(lane["capabilities"]) {
		if stringValue(item) == capability {
			return true
		}
	}
	return false
}

// laneAdapter resolves a lane's bare adapter seat name from its command argv0
// (basename, drop a .exe suffix), matching agentloop.LaneAdapterName and
// workflowtemplates.normalizeAdapterName. A lane with no command yields "".
// Shell-shim one-shot lanes (argv0 = sh/bash) resolve to the interpreter, not the
// inner CLI — those one-shot agy footguns are already covered by
// lintAgyOneShotPipeLane; this rule targets the declared agent-loop seat shape
// (direct argv, e.g. ["agy", "--dangerously-skip-permissions"]).
func laneAdapter(lane map[string]any) string {
	command := stringsFromSlice(lane["command"])
	if len(command) == 0 {
		return ""
	}
	base := filepath.Base(strings.TrimSpace(command[0]))
	return strings.TrimSuffix(base, ".exe")
}

func laneDeclaresAgentLoop(lane map[string]any) bool {
	if lane["agent_loop"] == true {
		return true
	}
	caps, ok := lane["adapter_capabilities"].(map[string]any)
	if !ok {
		return false
	}
	return caps["agent_loop"] == true
}

func laneHasSupervision(lane map[string]any) bool {
	_, exists := lane["supervision"]
	return exists
}

func laneIsAutonomousOrSupervised(lane map[string]any) bool {
	return laneDeclaresAgentLoop(lane) || laneHasSupervision(lane)
}

// LaneAllowsSharedCheckoutRepoWrite reports whether a lane records the explicit
// interactive-human compatibility override that permits a supervised/agent-loop
// repo-write lane to use the shared checkout. Validate enforces the rationale;
// this helper repeats the non-empty check so runtime snapshots fail closed when
// they were prepared before the validation rule existed.
func LaneAllowsSharedCheckoutRepoWrite(lane map[string]any) bool {
	return lane["allow_shared_checkout_repo_write"] == true &&
		strings.TrimSpace(stringValue(lane["shared_checkout_repo_write_rationale"])) != ""
}

// LaneRequiresWorktreeIsolationForAutonomousRepoWrite is the exported runtime
// predicate for the #242 launch gate. Plain operator-by-hand lanes remain
// warning-only; supervised or agent-loop lanes must either use per-job worktrees
// or carry the recorded shared-checkout compatibility override.
func LaneRequiresWorktreeIsolationForAutonomousRepoWrite(lane map[string]any) bool {
	if !laneIsAutonomousOrSupervised(lane) {
		return false
	}
	if lane["worktree_isolation"] == "per_job" {
		return false
	}
	return !LaneAllowsSharedCheckoutRepoWrite(lane)
}

func AutonomousWorktreeIsolationRefusalMessage(jobID string, laneID string) string {
	return "repo-write job '" + jobID + "' uses supervised/agent-loop lane '" + laneID + "' without worktree_isolation: per_job; autonomous repo-write lanes must use per-job worktrees. For an explicit interactive-human compatibility workflow, set lane allow_shared_checkout_repo_write=true and shared_checkout_repo_write_rationale to a non-empty reason."
}

func RefuseAutonomousSharedCheckoutRepoWrite(workflow map[string]any) error {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return nil
	}
	jobMap := WorkflowJobMap(workflow)
	for _, jobID := range sortedJobIDs(jobMap) {
		job := jobMap[jobID]
		if !jobIsRepoWrite(job) {
			continue
		}
		laneID := stringValue(job["lane_id"])
		if laneID == "" {
			continue
		}
		lane, ok := lanes[laneID].(map[string]any)
		if !ok || !LaneRequiresWorktreeIsolationForAutonomousRepoWrite(lane) {
			continue
		}
		return errf("%s", AutonomousWorktreeIsolationRefusalMessage(jobID, laneID))
	}
	return nil
}

// laneCommandIsAgyPrint reports whether the lane command invokes the `agy`
// binary with a `--print` (one-shot) flag. It tolerates both a direct argv
// (["agy", "--print", …]) and an `sh -c` stdin shim that execs agy.
// lintDeprecatedClaudePrintLane warns when a claude lane invokes the retired
// one-shot `--print`/`-p` mode. RFC 0088 / D148 retired `-p`/`--print`/`exec` for
// ALL lanes: every lane is now a daemon-owned long-lived interactive PTY
// agent-loop session. `claude --print` cannot run the work-packet loop — under
// the agent-loop executor it prints once and exits without claiming, and as a
// bare lane it never self-claims — so the lane silently parks/dies (the #148
// class). It must not be hardened; it must not be used. Fires regardless of
// adapter_capabilities.agent_loop because `--print` defeats the interactive loop
// even when wrapped.
func lintDeprecatedClaudePrintLane(workflow map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if !laneCommandIsClaudePrint(lane) {
			continue
		}
		// #199: this lint now mirrors a HARD REFUSAL in Validate (workflow
		// validate / run prepare) and the supervise-launch guard. A lane that
		// records the explicit `allow_claude_print: true` override is no longer
		// refused, so it no longer warrants the refusal finding either.
		if laneAllowsClaudePrint(lane) {
			continue
		}
		*findings = append(*findings, map[string]any{
			"rule":     "deprecated_claude_print_lane",
			"severity": "refusal",
			"message":  ClaudePrintRefusalMessage(laneID),
			"lane_id":  laneID,
		})
	}
}

// lintDeprecatedCodexExecLane warns when a Codex lane invokes the retired
// one-shot `codex exec` command. The hard refusal lives in
// RefuseCodexExecLane; this lint finding keeps `workflow lint` legible in the
// same style as the Claude one-shot guard.
func lintDeprecatedCodexExecLane(workflow map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if !laneCommandIsCodexExec(lane) {
			continue
		}
		*findings = append(*findings, map[string]any{
			"rule":     "deprecated_codex_exec_lane",
			"severity": "refusal",
			"message":  CodexExecRefusalMessage(laneID),
			"lane_id":  laneID,
		})
	}
}

// ClaudePrintRefusalMessage builds the hard-refusal diagnostic for a
// `claude --print`/`-p` lane. It is the single source of truth shared by
// workflowauthoring.Validate (which backs `workflow validate` and `run
// prepare`) and the supervise-launch guard, and it names the 2026-06-15
// API-token-burn cost consequence so the operator understands the refusal is
// about real money, not just a broken loop (#199).
func ClaudePrintRefusalMessage(laneID string) string {
	return "lane '" + laneID + "' runs `claude --print`/`-p`, the retired one-shot mode (RFC 0088 / D148) — REFUSED. " +
		"It cannot run the agent-loop interactive work-packet loop (it prints once and exits without claiming, so the lane silently parks/dies, the #148 class), " +
		"and after 2026-06-15 a live `claude --print` invocation bills against API tokens (real money per packet) instead of Claude plan/subscription usage. " +
		"Use a bare interactive command: [\"claude\", \"--dangerously-skip-permissions\"] with \"adapter_capabilities\": {\"agent_loop\": true}. " +
		"For a genuine compatibility case, set \"allow_claude_print\": true on the lane to override this refusal."
}

// laneAllowsClaudePrint reports whether a lane carries the explicit
// `allow_claude_print: true` override that opts out of the #199 refusal.
func laneAllowsClaudePrint(lane map[string]any) bool {
	return lane["allow_claude_print"] == true
}

// RefuseClaudePrintLane returns a hard error for a single lane that runs
// `claude --print`/`-p` without the explicit `allow_claude_print: true`
// override, or nil otherwise. It is the supervise-launch guard's choke point:
// the lane it is handed comes from a frozen workflow snapshot, so this is the
// last line of defense before the money-burning subprocess actually launches.
func RefuseClaudePrintLane(laneID string, lane map[string]any) error {
	if !laneCommandIsClaudePrint(lane) || laneAllowsClaudePrint(lane) {
		return nil
	}
	return errf("%s", ClaudePrintRefusalMessage(laneID))
}

// CodexExecRefusalMessage builds the hard-refusal diagnostic for a
// `codex exec` lane. `codex exec` is a one-shot command: it can consume one
// stdin packet, then exits or stalls outside the daemon-owned interactive
// work-packet loop. Use a bare interactive Codex command as an agent-loop lane.
func CodexExecRefusalMessage(laneID string) string {
	return "lane '" + laneID + "' runs `codex exec`, the retired one-shot mode (RFC 0088 / D148) — REFUSED. " +
		"It cannot run the daemon-owned agent-loop interactive work-packet loop and can stall before acking the delivered packet (#267). " +
		"Use a bare interactive command such as [\"codex\", \"--dangerously-bypass-approvals-and-sandbox\", \"--no-alt-screen\"] " +
		"with \"adapter_capabilities\": {\"agent_loop\": true} and \"supervision\": {\"transport\": \"pty_helper\"}."
}

// RefuseCodexExecLane returns a hard error for a single lane that invokes
// `codex exec`, or nil otherwise.
func RefuseCodexExecLane(laneID string, lane map[string]any) error {
	if !laneCommandIsCodexExec(lane) {
		return nil
	}
	return errf("%s", CodexExecRefusalMessage(laneID))
}

// RefuseRetiredOneShotLane is the shared launch-blocking guard for retired
// one-shot agent commands. It is used by workflow validation entry points,
// run.prepare, generator output validation, and supervise launch.
func RefuseRetiredOneShotLane(laneID string, lane map[string]any) error {
	if err := RefuseClaudePrintLane(laneID, lane); err != nil {
		return err
	}
	if err := RefuseCodexExecLane(laneID, lane); err != nil {
		return err
	}
	return nil
}

// RefuseClaudePrintLanes scans every lane in a workflow and returns a hard
// error for the first `claude --print`/`-p` lane that has not set the explicit
// `allow_claude_print: true` override. It is the single source of truth the
// `workflow validate` CLI and `run.prepare` call to escalate the
// deprecated_claude_print_lane lint finding from a warning into a REFUSAL
// (#199). Validate itself stays lint-clean so Lint can still surface the
// finding; the entry points call this to turn it into a launch-blocking error.
func RefuseClaudePrintLanes(workflow map[string]any) error {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return nil
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if err := RefuseClaudePrintLane(laneID, lane); err != nil {
			return err
		}
	}
	return nil
}

// RefuseCodexExecLanes scans every lane in a workflow and returns a hard error
// for the first `codex exec` lane. It exists for tests and compatibility with
// the Claude-specific helper; new call sites should use
// RefuseRetiredOneShotLanes.
func RefuseCodexExecLanes(workflow map[string]any) error {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return nil
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if err := RefuseCodexExecLane(laneID, lane); err != nil {
			return err
		}
	}
	return nil
}

// RefuseRetiredOneShotLanes scans every lane in a workflow and returns a hard
// error for the first retired one-shot agent command.
func RefuseRetiredOneShotLanes(workflow map[string]any) error {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return nil
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		if err := RefuseRetiredOneShotLane(laneID, lane); err != nil {
			return err
		}
	}
	return nil
}

type adapterFlagRule struct {
	flag           string
	allowedAdapter string
	label          string
}

var adapterSpecificUnsafeFlags = []adapterFlagRule{
	{
		flag:           "--dangerously-skip-permissions",
		allowedAdapter: "claude",
		label:          "Claude",
	},
	{
		flag:           "--permission-mode",
		allowedAdapter: "claude",
		label:          "Claude",
	},
	{
		flag:           "--dangerously-bypass-approvals-and-sandbox",
		allowedAdapter: "codex",
		label:          "Codex",
	},
	{
		flag:           "--yolo",
		allowedAdapter: "codex",
		label:          "Codex",
	},
}

var knownAdapterFlagSeats = map[string]bool{
	"agy":    true,
	"claude": true,
	"codex":  true,
}

// lintAdapterFlagMismatches refuses workflow-authored commands that put a
// provider-specific unsafe flag on a different provider's lane. These mistakes
// launch successfully enough to burn a run but fail inside the adapter CLI, so
// the scaffold has to reject them before a supervised lane is spawned.
func lintAdapterFlagMismatches(workflow map[string]any, findings *[]map[string]any) {
	lanes, ok := workflow["lanes"].(map[string]any)
	if !ok {
		return
	}
	for _, laneID := range sortedLaneIDs(lanes) {
		lane, ok := lanes[laneID].(map[string]any)
		if !ok {
			continue
		}
		adapter := laneAdapter(lane)
		if !knownAdapterFlagSeats[adapter] {
			continue
		}
		for _, flag := range laneCommandFlags(lane) {
			rule, ok := adapterFlagRuleFor(flag)
			if !ok || rule.allowedAdapter == adapter {
				continue
			}
			*findings = append(*findings, map[string]any{
				"rule":            "adapter_flag_mismatch",
				"severity":        "refusal",
				"message":         adapterFlagMismatchRefusalMessage(laneID, adapter, flag, rule),
				"lane_id":         laneID,
				"adapter":         adapter,
				"flag":            flag,
				"allowed_adapter": rule.allowedAdapter,
			})
		}
	}
}

func adapterFlagMismatchRefusalMessage(laneID string, adapter string, flag string, rule adapterFlagRule) string {
	return fmt.Sprintf("lane %q runs adapter %q with %s-only unsafe flag %q — REFUSED. Use an adapter-specific command for this lane; do not carry Claude or Codex unsafe flags across adapters.", laneID, adapter, rule.label, flag)
}

func adapterFlagRuleFor(flag string) (adapterFlagRule, bool) {
	for _, rule := range adapterSpecificUnsafeFlags {
		if rule.flag == flag {
			return rule, true
		}
	}
	return adapterFlagRule{}, false
}

func laneCommandFlags(lane map[string]any) []string {
	flags := []string{}
	for _, arg := range stringsFromSlice(lane["command"]) {
		for _, field := range strings.Fields(arg) {
			flag := strings.Trim(field, "\"'")
			if !strings.HasPrefix(flag, "-") {
				continue
			}
			if before, _, ok := strings.Cut(flag, "="); ok {
				flag = before
			}
			flags = append(flags, flag)
		}
	}
	return flags
}

// laneCommandIsClaudePrint reports whether a lane invokes the `claude` binary
// with a one-shot `--print`/`-p` flag. Uses laneAdapter (basename of command[0])
// so an absolute claude path is still detected.
func laneCommandIsClaudePrint(lane map[string]any) bool {
	if laneAdapter(lane) != "claude" {
		return false
	}
	for _, arg := range stringsFromSlice(lane["command"]) {
		for _, field := range strings.Fields(arg) {
			switch strings.Trim(field, "\"'") {
			case "--print", "-p":
				return true
			}
		}
	}
	return false
}

func laneCommandIsCodexExec(lane map[string]any) bool {
	tokens := laneCommandTokens(lane)
	for idx := 0; idx+1 < len(tokens); idx++ {
		if normalizedCommandToken(tokens[idx]) == "codex" && normalizedCommandToken(tokens[idx+1]) == "exec" {
			return true
		}
	}
	return false
}

func laneCommandTokens(lane map[string]any) []string {
	tokens := []string{}
	for _, arg := range stringsFromSlice(lane["command"]) {
		tokens = append(tokens, strings.Fields(arg)...)
	}
	return tokens
}

func normalizedCommandToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'`")
	token = strings.Trim(token, ";&|()")
	if token == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(token), ".exe")
}

func laneCommandIsAgyPrint(lane map[string]any) bool {
	command := stringsFromSlice(lane["command"])
	if len(command) == 0 {
		return false
	}
	invokesAgy := false
	usesPrint := false
	for _, arg := range command {
		for _, field := range strings.Fields(arg) {
			switch strings.Trim(field, "\"'") {
			case "agy":
				invokesAgy = true
			case "--print", "-p":
				usesPrint = true
			}
		}
	}
	return invokesAgy && usesPrint
}

func sortedLaneIDs(lanes map[string]any) []string {
	ids := make([]string, 0, len(lanes))
	for laneID := range lanes {
		ids = append(ids, laneID)
	}
	sort.Strings(ids)
	return ids
}

func invalidLintCoverage() map[string]any {
	checks := []map[string]any{
		lintCoverageCheck("reviewer_independence", false, "workflow is invalid; reviewer independence was not evaluated"),
		lintCoverageCheck("fresh_context", false, "workflow is invalid; review context freshness was not evaluated"),
		lintCoverageCheck("write_isolation", false, "workflow is invalid; write isolation was not evaluated"),
		lintCoverageCheck("revision_or_escalation_path", false, "workflow is invalid; revision and escalation paths were not evaluated"),
		lintCoverageCheck("posture_diversity", false, "workflow is invalid; review posture diversity was not evaluated"),
	}
	return map[string]any{"score": 0, "max_score": len(checks), "level": "weak", "checks": checks}
}

func lintCoverage(workflow map[string]any, jobMap map[string]map[string]any, findings []map[string]any) map[string]any {
	rules := map[string]bool{}
	for _, finding := range findings {
		rules[stringValue(finding["rule"])] = true
	}
	reviewJobs := []map[string]any{}
	for _, job := range jobMap {
		if verdictJobTypes[defaultString(job["type"], "generic")] {
			reviewJobs = append(reviewJobs, job)
		}
	}
	reviewerIndependent := !rules["same_model_review_pair"] && !rules["same_model_revision_cycle"] && !rules["same_model_adjudicator_pair"]
	freshContext := !rules["review_without_fresh_context"]
	writeIsolated := !rules["broad_write_scope"] && !rules["repo_write_without_worktree_isolation"]
	hasRevisionOrEscalation := !rules["missing_review_escalation_path"]
	checks := []map[string]any{
		lintCoverageCheck("reviewer_independence", reviewerIndependent, coverageReason(len(reviewJobs) == 0, reviewerIndependent, "workflow has no review jobs", "review lanes are model-family independent", "one or more review lanes share model family with implementation work")),
		lintCoverageCheck("fresh_context", freshContext, coverageReason(len(reviewJobs) == 0, freshContext, "workflow has no review jobs", "review jobs require fresh context", "one or more review jobs can reuse contaminated context")),
		lintCoverageCheck("write_isolation", writeIsolated, coverageReason(false, writeIsolated, "", "repo-write jobs are narrowly scoped and isolated", "one or more repo-write jobs are broad or lack per-job isolation")),
		lintCoverageCheck("revision_or_escalation_path", hasRevisionOrEscalation, coverageReason(len(reviewJobs) == 0, hasRevisionOrEscalation, "workflow has no review jobs", "review verdicts have a revision or human escalation path", "review verdicts lack a revision or human escalation path")),
		lintCoverageCheck("posture_diversity", hasReviewPostureDiversity(reviewJobs), reviewPostureDiversityReason(reviewJobs)),
	}
	score := 0
	for _, check := range checks {
		if check["passed"] == true {
			score++
		}
	}
	return map[string]any{"score": score, "max_score": len(checks), "level": lintCoverageLevel(score, len(checks)), "checks": checks}
}

func lintCoverageCheck(id string, passed bool, reason string) map[string]any {
	return map[string]any{"id": id, "passed": passed, "weight": 1, "reason": reason}
}

func coverageReason(empty bool, passed bool, emptyReason string, passedReason string, failedReason string) string {
	if empty {
		return emptyReason
	}
	if passed {
		return passedReason
	}
	return failedReason
}

func hasReviewPostureDiversity(reviewJobs []map[string]any) bool {
	if len(reviewJobs) == 0 {
		return true
	}
	postures := map[string]bool{}
	for _, job := range reviewJobs {
		posture := stringValue(job["review_posture"])
		if posture == "" {
			posture = "neutral"
		}
		postures[posture] = true
	}
	return len(postures) >= 2
}

func reviewPostureDiversityReason(reviewJobs []map[string]any) string {
	if len(reviewJobs) == 0 {
		return "workflow has no review jobs"
	}
	postures := []string{}
	seen := map[string]bool{}
	for _, job := range reviewJobs {
		posture := stringValue(job["review_posture"])
		if posture == "" {
			posture = "neutral"
		}
		if !seen[posture] {
			seen[posture] = true
			postures = append(postures, posture)
		}
	}
	sort.Strings(postures)
	if len(postures) >= 2 {
		return "review jobs cover multiple postures: " + strings.Join(postures, ", ")
	}
	return "review jobs cover only one posture: " + postures[0]
}

func lintCoverageLevel(score int, maxScore int) string {
	if maxScore <= 0 {
		return "weak"
	}
	if score == maxScore {
		return "strong"
	}
	if score >= max(1, (maxScore*3+4)/5) {
		return "adequate"
	}
	return "weak"
}

func modelFamily(value string) string {
	tokens := []string{}
	for _, token := range modelTokenPattern.Split(strings.ToLower(value), -1) {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return strings.ToLower(value)
	}
	if (tokens[0] == "openai" || tokens[0] == "anthropic" || tokens[0] == "google") && len(tokens) > 1 {
		return tokens[1]
	}
	return tokens[0]
}

func laneModelFamily(lanes map[string]any, laneID string) string {
	if laneID == "" {
		return ""
	}
	lane, ok := lanes[laneID].(map[string]any)
	if !ok {
		return ""
	}
	source := stringValue(lane["display_model"])
	if source == "" {
		source = stringValue(lane["model"])
	}
	if source == "" {
		source = laneID
	}
	return modelFamily(source)
}

func jobIsRepoWrite(job map[string]any) bool {
	scope, ok := job["write_scope"].(map[string]any)
	if !ok {
		return false
	}
	return scope["repo_write"] == true || stringValue(scope["mode"]) == "repo_write"
}

func effectiveFreshSessionRequired(job map[string]any) bool {
	if job["fresh_session_required"] == true {
		return true
	}
	return defaultString(job["type"], "generic") == "review" && job["reviewer_context_policy"] == "fresh" && job["fresh_session_required"] == nil
}

func sortedJobIDs(jobMap map[string]map[string]any) []string {
	ids := make([]string, 0, len(jobMap))
	for jobID := range jobMap {
		ids = append(ids, jobID)
	}
	sort.Strings(ids)
	return ids
}

func sortedKeysFromClaims(values map[string][]sharedResourceClaim) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysFromStringSlices(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
