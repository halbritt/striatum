package reads

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const evidenceFreeTextPlaceholder = "<redacted-free-text>"

var tokenLikePattern = regexp.MustCompile(`\b(?:[A-Fa-f0-9]{40,}|[A-Za-z0-9+/]{48,}={0,2})\b`)
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

var allowedRedactionTiers = map[string]bool{
	"public":   true,
	"curated":  true,
	"internal": true,
}

var deniedCorpusNames = map[string]bool{
	".env": true,
}

var deniedCorpusParts = map[string]bool{
	".striatum":        true,
	"transcripts":      true,
	"terminal_output":  true,
	"raw_model_output": true,
}

var deniedCorpusSuffixes = []string{".pem", ".key", ".p12", ".sqlite3", ".sqlite", ".db", ".swp", ".swo"}

var safeEventFields = map[string]bool{
	"event_id":        true,
	"event_type":      true,
	"run_id":          true,
	"job_id":          true,
	"workflow_job_id": true,
	"session_id":      true,
	"artifact_id":     true,
	"verdict_id":      true,
	"blocker_kind":    true,
	"state":           true,
	"created_at":      true,
	"request_hash":    true,
	"response_hash":   true,
	"row_hash":        true,
	"previous_hash":   true,
	"repository_id":   true,
}

func redactionTierFromEnvelope(params map[string]any) (string, error) {
	tier := "public"
	if raw, ok := params["redaction_tier"].(string); ok && raw != "" {
		tier = raw
	}
	if !allowedRedactionTiers[tier] {
		return "", fmt.Errorf("invalid redaction_tier: %s", tier)
	}
	return tier, nil
}

func validateCorpusSourcePath(path string) error {
	clean := filepath.ToSlash(path)
	lower := strings.ToLower(clean)
	parts := strings.Split(lower, "/")
	name := ""
	if len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	if deniedCorpusNames[name] || strings.HasPrefix(name, ".env.") {
		return fmt.Errorf("corpus source path is denied: %s", path)
	}
	for _, suffix := range deniedCorpusSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return fmt.Errorf("corpus source path is denied: %s", path)
		}
	}
	for _, part := range parts {
		if deniedCorpusParts[part] {
			return fmt.Errorf("corpus source path is denied: %s", path)
		}
	}
	if strings.HasPrefix(name, "transcript") &&
		(strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".log")) {
		return fmt.Errorf("corpus source path is denied: %s", path)
	}
	return nil
}

func redactCommitMessage(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(stripped), "co-authored-by:") &&
			strings.Contains(stripped, "<") && strings.Contains(stripped, ">") {
			continue
		}
		if tokenLikePattern.MatchString(stripped) && tokenLikePattern.FindString(stripped) == stripped {
			continue
		}
		lines = append(lines, tokenLikePattern.ReplaceAllString(line, "<redacted-token>"))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func redactGeneratedCorpusText(text string) string {
	text = strings.ToValidUTF8(text, "\uFFFD")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = ansiEscapePattern.ReplaceAllString(text, "")
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		line = tokenLikePattern.ReplaceAllString(line, "<redacted-token>")
		lines[index] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func redactEventPayload(payload map[string]any) map[string]any {
	redacted := map[string]any{}
	for key, value := range payload {
		if safeEventFields[key] {
			redacted[key] = value
		}
	}
	return redacted
}

func redactEvidencePayload(payload map[string]any) map[string]any {
	redacted := map[string]any{}
	for key, value := range payload {
		result := applyEvidencePolicy(value, evidencePolicyForTopLevel(key, value))
		if result == evidenceDrop {
			continue
		}
		redacted[key] = result
	}
	return redacted
}

type evidenceDropSentinel struct{}

var evidenceDrop = evidenceDropSentinel{}

type objectPolicy map[string]any

func evidencePolicyForTopLevel(key string, value any) any {
	if key == "jobs" {
		if _, ok := value.([]map[string]any); ok {
			return snapshotJobsPolicy()
		}
		if _, ok := value.([]any); ok {
			return snapshotJobsPolicy()
		}
		return objectPolicy{"_dict": "safe"}
	}
	policies := map[string]any{
		"runs":                                 objectPolicy{"_each": objectPolicy{"run_id": "safe", "state": "safe", "branch_name": "safe"}},
		"open_blockers":                        blockerListPolicy(),
		"human_checkpoints":                    blockerListPolicy(),
		"latest_non_accepting_review_verdicts": verdictListPolicy(true),
		"claimable_jobs":                       objectPolicy{"_each": objectPolicy{"role_id": "safe", "lane_id": "safe", "count": "safe", "workflow_job_ids": objectPolicy{"_items": "safe"}}},
		"blocked_downstream_jobs":              blockedDownstreamPolicy(),
		"next_actions":                         objectPolicy{"_items": "safe"},
		"ok":                                   "safe",
		"availability_ok":                      "safe",
		"provenance_ok":                        "safe",
		"schema_version":                       "safe",
		"problems":                             objectPolicy{"_items": "safe"},
		"exported_at":                          "safe",
		"workflow":                             objectPolicy{"workflow_id": "safe", "workflow_version": "safe"},
		"run":                                  objectPolicy{"run_id": "safe", "branch_name": "safe", "state": "safe"},
		"artifacts":                            artifactListPolicy(),
		"sessions":                             sessionListPolicy(),
		"verdicts":                             verdictListPolicy(false),
		"blockers":                             blockerListPolicy(),
		"doctor":                               objectPolicy{"ok": "safe", "availability_ok": "safe", "provenance_ok": "safe", "problems": objectPolicy{"_items": "safe"}},
	}
	if policy, ok := policies[key]; ok {
		return policy
	}
	return "redacted"
}

func snapshotJobsPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"job_id":                 "safe",
		"workflow_job_id":        "safe",
		"job_type":               "safe",
		"role_id":                "safe",
		"lane":                   "safe",
		"display_model":          "safe",
		"author":                 authorPolicy(),
		"state":                  "safe",
		"attempt":                "safe",
		"max_attempts":           "safe",
		"fresh_session_required": "safe",
		"title":                  "redacted",
		"dependencies":           dependencyListPolicy(),
	}}
}

func artifactListPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"artifact_id":    "safe",
		"job_id":         "safe",
		"session_id":     "safe",
		"logical_name":   "safe",
		"artifact_kind":  "safe",
		"kind":           "safe",
		"repo_path":      "safe",
		"path":           "safe",
		"content_sha256": "safe",
		"byline":         "safe",
		"published_at":   "safe",
		"created_at":     "safe",
		"author":         authorPolicy(),
	}}
}

func authorPolicy() objectPolicy {
	return objectPolicy{
		"role_id":            "safe",
		"lane_id":            "safe",
		"display_model":      "safe",
		"workflow_job_id":    "safe",
		"ordinal":            "safe",
		"line":               "safe",
		"actual_author_line": "safe",
	}
}

func sessionListPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"session_id":       "safe",
		"role_id":          "safe",
		"lane_id":          "safe",
		"slug":             "safe",
		"ordinal":          "safe",
		"state":            "safe",
		"registered_at":    "safe",
		"closed_at":        "safe",
		"close_reason":     "redacted",
		"non_fresh_reason": "redacted",
	}}
}

func verdictListPolicy(includeRun bool) objectPolicy {
	policy := objectPolicy{
		"verdict_id":                   "safe",
		"job_id":                       "safe",
		"session_id":                   "safe",
		"verdict":                      "safe",
		"findings_artifact_id":         "safe",
		"rationale":                    "redacted",
		"posture":                      "safe",
		"review_posture":               "safe",
		"recorded_at":                  "safe",
		"created_at":                   "safe",
		"model_identity_declared":      "safe",
		"model_family_at_record":       "safe",
		"model_identity_basis":         "safe",
		"model_co_blindness_at_record": "safe",
	}
	if includeRun {
		policy["run_id"] = "safe"
		policy["workflow_job_id"] = "safe"
		policy["job_state"] = "safe"
	}
	return objectPolicy{"_each": policy}
}

func blockerListPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"blocker_id":           "safe",
		"run_id":               "safe",
		"job_id":               "safe",
		"session_id":           "safe",
		"severity":             "safe",
		"blocker_kind":         "safe",
		"description":          "redacted",
		"state":                "safe",
		"workflow_job_id":      "safe",
		"job_state":            "safe",
		"created_at":           "safe",
		"resolve_actions":      objectPolicy{"_items": "safe"},
		"resolve_action_hints": objectPolicy{"_dict": "safe"},
	}}
}

func dependencyListPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"depends_on_job_id": "safe",
		"workflow_job_id":   "safe",
		"state":             "safe",
		"required_verdicts": objectPolicy{"_items": "safe"},
		"latest_verdict":    "safe",
	}}
}

func blockedDownstreamPolicy() objectPolicy {
	return objectPolicy{"_each": objectPolicy{
		"job_id":          "safe",
		"workflow_job_id": "safe",
		"state":           "safe",
		"role_id":         "safe",
		"lane":            "safe",
		"blocked_by":      dependencyListPolicy(),
	}}
}

func applyEvidencePolicy(value any, policy any) any {
	switch policy {
	case "safe":
		switch value.(type) {
		case map[string]any, []any, []map[string]any:
			return evidenceFreeTextPlaceholder
		default:
			return value
		}
	case "redacted":
		if value == nil {
			return nil
		}
		return evidenceFreeTextPlaceholder
	case "dropped":
		return evidenceDrop
	}
	object, ok := policy.(objectPolicy)
	if !ok {
		if value == nil {
			return nil
		}
		return evidenceFreeTextPlaceholder
	}
	switch typed := value.(type) {
	case []map[string]any:
		if childPolicy, ok := object["_each"]; ok {
			out := make([]any, 0, len(typed))
			for _, item := range typed {
				result := applyEvidencePolicy(item, childPolicy)
				if result != evidenceDrop {
					out = append(out, result)
				}
			}
			return out
		}
	case []any:
		key := "_items"
		if _, ok := object["_each"]; ok {
			key = "_each"
		}
		if childPolicy, ok := object[key]; ok {
			out := make([]any, 0, len(typed))
			for _, item := range typed {
				result := applyEvidencePolicy(item, childPolicy)
				if result != evidenceDrop {
					out = append(out, result)
				}
			}
			return out
		}
		return evidenceFreeTextPlaceholder
	case map[string]any:
		if childPolicy, ok := object["_dict"]; ok {
			out := map[string]any{}
			for key, childValue := range typed {
				result := applyEvidencePolicy(childValue, childPolicy)
				if result != evidenceDrop {
					out[key] = result
				}
			}
			return out
		}
		out := map[string]any{}
		for key, childValue := range typed {
			childPolicy, ok := object[key]
			if !ok {
				childPolicy = "redacted"
			}
			result := applyEvidencePolicy(childValue, childPolicy)
			if result != evidenceDrop {
				out[key] = result
			}
		}
		return out
	default:
		if value == nil {
			return nil
		}
		return evidenceFreeTextPlaceholder
	}
	return evidenceFreeTextPlaceholder
}

func redactRunSummaryPayload(payload map[string]any) map[string]any {
	redacted := map[string]any{}
	for key, value := range payload {
		redacted[key] = value
	}
	if status, ok := payload["status"].(map[string]any); ok {
		redacted["status"] = redactEvidencePayload(status)
	}
	if doctor, ok := payload["doctor"].(map[string]any); ok {
		redacted["doctor"] = redactEvidencePayload(doctor)
	}
	if blockers, ok := toMapSlice(payload["blockers"]); ok {
		clean := make([]any, 0, len(blockers))
		for _, blocker := range blockers {
			clean = append(clean, map[string]any{
				"blocker_id":   blocker["blocker_id"],
				"job_id":       blocker["job_id"],
				"severity":     blocker["severity"],
				"blocker_kind": blocker["blocker_kind"],
				"state":        blocker["state"],
			})
		}
		redacted["blockers"] = clean
	}
	if verdicts, ok := toMapSlice(payload["verdicts"]); ok {
		clean := make([]any, 0, len(verdicts))
		for _, entry := range verdicts {
			clean = append(clean, safeVerdict(entry))
		}
		redacted["verdicts"] = clean
	}
	if artifacts, ok := toMapSlice(payload["artifacts"]); ok {
		clean := make([]any, 0, len(artifacts))
		for _, entry := range artifacts {
			clean = append(clean, safeArtifact(entry))
		}
		redacted["artifacts"] = clean
	}
	if sessions, ok := toMapSlice(payload["sessions"]); ok {
		clean := make([]any, 0, len(sessions))
		for _, entry := range sessions {
			clean = append(clean, safeSession(entry))
		}
		redacted["sessions"] = clean
	}
	if timing, ok := payload["timing"].(map[string]any); ok {
		clean := map[string]any{}
		for key, value := range timing {
			clean[key] = value
		}
		if clean["completed_at"] == nil || clean["completed_at"] == "" {
			clean["duration"] = nil
		}
		redacted["timing"] = clean
	}
	allowed := map[string]bool{
		"status":                   true,
		"doctor":                   true,
		"artifacts":                true,
		"sessions":                 true,
		"verdicts":                 true,
		"verdicts_by_workflow_job": true,
		"blockers":                 true,
		"branch_context":           true,
		"timing":                   true,
	}
	for key := range redacted {
		if !allowed[key] {
			redacted[key] = evidenceFreeTextPlaceholder
		}
	}
	return redacted
}

func safeVerdict(entry map[string]any) map[string]any {
	return map[string]any{
		"verdict_id":                   entry["verdict_id"],
		"job_id":                       entry["job_id"],
		"workflow_job_id":              entry["workflow_job_id"],
		"verdict":                      entry["verdict"],
		"findings_artifact_id":         entry["findings_artifact_id"],
		"created_at":                   entry["created_at"],
		"recorded_at":                  entry["recorded_at"],
		"posture":                      entry["posture"],
		"review_posture":               entry["review_posture"],
		"model_identity_declared":      entry["model_identity_declared"],
		"model_family_at_record":       entry["model_family_at_record"],
		"model_identity_basis":         entry["model_identity_basis"],
		"model_co_blindness_at_record": entry["model_co_blindness_at_record"],
	}
}

func safeArtifact(entry map[string]any) map[string]any {
	return map[string]any{
		"artifact_id":    entry["artifact_id"],
		"job_id":         entry["job_id"],
		"logical_name":   entry["logical_name"],
		"artifact_kind":  entry["artifact_kind"],
		"repo_path":      entry["repo_path"],
		"content_sha256": entry["content_sha256"],
		"size_bytes":     entry["size_bytes"],
		"created_at":     entry["created_at"],
	}
}

func safeSession(entry map[string]any) map[string]any {
	result := map[string]any{
		"session_id":    entry["session_id"],
		"role_id":       entry["role_id"],
		"lane_id":       entry["lane_id"],
		"slug":          entry["slug"],
		"ordinal":       entry["ordinal"],
		"state":         entry["state"],
		"registered_at": entry["registered_at"],
		"closed_at":     entry["closed_at"],
	}
	for _, key := range []string{"close_reason", "non_fresh_reason"} {
		if entry[key] == nil || entry[key] == "" {
			result[key] = entry[key]
		} else {
			result[key] = evidenceFreeTextPlaceholder
		}
	}
	return result
}

func redactArchiveRow(kind string, row map[string]any) map[string]any {
	redacted := map[string]any{}
	for key, value := range row {
		redacted[key] = value
	}
	switch kind {
	case "sessions":
		for _, key := range []string{"close_reason", "non_fresh_reason"} {
			if redacted[key] != nil && redacted[key] != "" {
				redacted[key] = evidenceFreeTextPlaceholder
			}
		}
	case "jobs":
		if redacted["title"] != nil && redacted["title"] != "" {
			redacted["title"] = evidenceFreeTextPlaceholder
		}
	case "verdicts":
		if redacted["rationale"] != nil && redacted["rationale"] != "" {
			redacted["rationale"] = evidenceFreeTextPlaceholder
		}
	case "blockers":
		if redacted["description"] != nil && redacted["description"] != "" {
			redacted["description"] = evidenceFreeTextPlaceholder
		}
		if payload, ok := redacted["payload_json"].(map[string]any); ok {
			redacted["payload_json"] = redactEvidencePayload(payload)
		}
	case "events":
		if payload, ok := redacted["payload_json"].(map[string]any); ok {
			redacted["payload_json"] = redactEventPayload(payload)
		}
	case "queue_messages", "work_packets", "command_requests", "process_executions", "process_supervisors", "process_supervisor_pointers":
		for _, key := range []string{"payload_json", "metadata_json", "pointer_metadata_json", "supervisor_metadata_json"} {
			if payload, ok := redacted[key].(map[string]any); ok {
				redacted[key] = redactEvidencePayload(payload)
			}
		}
	}
	return redacted
}

func redactArchiveRows(kind string, rows []map[string]any) []map[string]any {
	redacted := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		redacted = append(redacted, redactArchiveRow(kind, row))
	}
	return redacted
}

func toMapSlice(value any) ([]map[string]any, bool) {
	switch typed := value.(type) {
	case []map[string]any:
		return typed, true
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			out = append(out, row)
		}
		return out, true
	default:
		return nil, false
	}
}
