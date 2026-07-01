package workflowgenerate

import (
	"fmt"
	"path"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

type generatedPlacementPolicy struct {
	Posture        string
	BlobConfigured *bool
	FallbackToGit  bool
}

func placementPostureOption(options map[string]any) (string, error) {
	posture := ""
	postureField := ""
	for _, key := range []string{"artifact_placement_posture", "artifact_blob_posture", "blob_posture"} {
		value, exists := options[key]
		if !exists || strings.TrimSpace(fmt.Sprint(value)) == "" || fmt.Sprint(value) == "<nil>" {
			continue
		}
		normalized, ok := artifactcontracts.NormalizePlacementPosture(value)
		if !ok {
			return "", genErr(
				"unknown artifact placement posture; valid values: "+strings.Join(artifactcontracts.AllowedPlacementPostureList(), ", "),
				"spec.options."+key,
			)
		}
		if posture != "" && posture != normalized {
			return "", genErr("conflicting artifact placement posture options", "spec.options."+key)
		}
		posture = normalized
		postureField = key
	}
	if value, exists := options["blob_required"]; exists {
		required, ok := boolFrom(value)
		if !ok {
			return "", genErr("blob_required must be a boolean", "spec.options.blob_required")
		}
		if required {
			if posture != "" && posture != artifactcontracts.BlobRequiredPosture {
				return "", genErr("blob_required conflicts with "+postureField, "spec.options.blob_required")
			}
			posture = artifactcontracts.BlobRequiredPosture
		}
	}
	return posture, nil
}

func optionalBoolOption(options map[string]any, key string) (*bool, error) {
	value, exists := options[key]
	if !exists {
		return nil, nil
	}
	parsed, ok := boolFrom(value)
	if !ok {
		return nil, genErr(key+" must be a boolean", "spec.options."+key)
	}
	return &parsed, nil
}

func placementPolicyForSpec(spec Spec) (generatedPlacementPolicy, error) {
	policy := generatedPlacementPolicy{
		Posture:        spec.PlacementPosture,
		BlobConfigured: spec.BlobConfigured,
	}
	if policy.Posture == "" {
		if policy.BlobConfigured != nil && !*policy.BlobConfigured {
			policy.Posture = artifactcontracts.GitCompatiblePosture
			policy.FallbackToGit = true
			return policy, nil
		}
		policy.Posture = artifactcontracts.BlobPreferredPosture
		return policy, nil
	}
	if policy.Posture == artifactcontracts.BlobRequiredPosture {
		if policy.BlobConfigured == nil || !*policy.BlobConfigured {
			return generatedPlacementPolicy{}, genErr(
				"artifact_placement_posture=blob_required requires blob_configured=true; configure this repository with repo add --apply-blob-creation or use artifact_placement_posture=git_compatible",
				"spec.options.blob_configured",
			)
		}
		return policy, nil
	}
	if policy.Posture == artifactcontracts.BlobPreferredPosture && policy.BlobConfigured != nil && !*policy.BlobConfigured {
		policy.Posture = artifactcontracts.GitCompatiblePosture
		policy.FallbackToGit = true
	}
	return policy, nil
}

func applyGeneratedPlacementPolicy(jobs []map[string]any, edges []map[string]any, policy generatedPlacementPolicy) {
	for _, job := range jobs {
		for _, artifactItem := range listFrom(job["expected_artifacts"]) {
			artifact := mapFrom(artifactItem)
			if policy.Posture == artifactcontracts.GitCompatiblePosture && artifact["placement"] == artifactcontracts.PlacementBlobExhaust {
				artifact["placement"] = artifactcontracts.PlacementGitPublication
			}
			if policy.Posture == artifactcontracts.BlobRequiredPosture && artifact["placement"] == artifactcontracts.PlacementBlobExhaust {
				artifact["artifact_placement_posture"] = artifactcontracts.BlobRequiredPosture
			}
		}
	}
	seedBlobArtifactInputs(jobs, edges)
}

func seedBlobArtifactInputs(jobs []map[string]any, edges []map[string]any) {
	jobsByID := map[string]map[string]any{}
	for _, job := range jobs {
		jobsByID[fmt.Sprint(job["id"])] = job
	}
	for _, edge := range edges {
		fromID := fmt.Sprint(edge["from"])
		toID := fmt.Sprint(edge["to"])
		upstream := jobsByID[fromID]
		downstream := jobsByID[toID]
		if upstream == nil || downstream == nil {
			continue
		}
		for _, artifactItem := range listFrom(upstream["expected_artifacts"]) {
			artifact := mapFrom(artifactItem)
			if artifact["placement"] != artifactcontracts.PlacementBlobExhaust {
				continue
			}
			appendBlobArtifactInput(downstream, fromID, artifact)
		}
	}
}

func appendBlobArtifactInput(job map[string]any, fromID string, artifact map[string]any) {
	next := append([]any{}, listFrom(job["inputs"])...)
	key := blobInputKey(fromID, artifact)
	for _, item := range next {
		if blobInputKey(fmt.Sprint(mapFrom(item)["from"]), mapFrom(item)) == key {
			return
		}
	}
	next = append(next, map[string]any{
		"from":           fromID,
		"logical_name":   artifact["logical_name"],
		"kind":           artifact["kind"],
		"path":           artifact["path"],
		"placement":      artifactcontracts.PlacementBlobExhaust,
		"content_access": "artifact.get_content",
		"artifact_lookup": map[string]any{
			"method": "artifact.list_for_run",
			"match": map[string]any{
				"workflow_job_id": fromID,
				"logical_name":    artifact["logical_name"],
				"path":            artifact["path"],
			},
		},
	})
	job["inputs"] = next
}

func blobInputKey(fromID string, artifact map[string]any) string {
	return strings.Join([]string{
		fromID,
		fmt.Sprint(artifact["logical_name"]),
		fmt.Sprint(artifact["kind"]),
		fmt.Sprint(artifact["path"]),
	}, "\x00")
}

func generatedDialogueExhaustArtifact(artifactKind, artifactPath string) bool {
	kind := strings.TrimSpace(artifactKind)
	if kind != "handoff" && kind != "progress_note" {
		return false
	}
	clean := "/" + strings.Trim(strings.ToLower(path.Clean(artifactPath)), "/") + "/"
	for _, segment := range []string{
		"/dialogue/",
		"/cross_exam/",
		"/scorecards/",
		"/audit/",
	} {
		if strings.Contains(clean, segment) {
			return true
		}
	}
	return false
}
