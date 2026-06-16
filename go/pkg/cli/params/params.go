package params

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Options struct {
	RepositoryID string
}

func Build(group string, args []string, options Options) (map[string]any, error) {
	result := map[string]any{}
	if options.RepositoryID != "" {
		result["repository_id"] = options.RepositoryID
	}
	positionals, err := parseFlags(args, result)
	if err != nil {
		return nil, err
	}
	names := positionalNames(group)
	for i, value := range positionals {
		if i < len(names) {
			result[names[i]] = value
			continue
		}
		existing, _ := result["args"].([]any)
		result["args"] = append(existing, value)
	}
	applyAliases(group, result)
	return result, nil
}

func parseFlags(args []string, result map[string]any) ([]string, error) {
	positionals := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			positionals = append(positionals, arg)
			continue
		}
		keyValue := strings.TrimPrefix(arg, "--")
		if keyValue == "" {
			return nil, fmt.Errorf("empty flag name")
		}
		if strings.HasPrefix(keyValue, "no-") && !strings.Contains(keyValue, "=") {
			setValue(result, normalizeKey(strings.TrimPrefix(keyValue, "no-")), false)
			continue
		}
		key, value, hasValue := strings.Cut(keyValue, "=")
		key = normalizeKey(key)
		if key == "" {
			return nil, fmt.Errorf("empty flag name")
		}
		if !hasValue {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				value = args[i]
			} else {
				setValue(result, key, true)
				continue
			}
		}
		setValue(result, key, coerceValue(key, value))
	}
	return positionals, nil
}

func normalizeKey(key string) string {
	return strings.ReplaceAll(strings.TrimSpace(key), "-", "_")
}

func coerceValue(key string, value string) any {
	if key == "body_json" || key == "content" || key == "patch" {
		return value
	}
	if key == "spec" || strings.HasSuffix(key, "_json") {
		var decoded any
		if err := json.Unmarshal([]byte(value), &decoded); err == nil {
			return decoded
		}
	}
	switch strings.ToLower(value) {
	case "true":
		return true
	case "false":
		return false
	}
	if parsed, err := strconv.Atoi(value); err == nil {
		return parsed
	}
	return value
}

func setValue(result map[string]any, key string, value any) {
	if existing, ok := result[key]; ok {
		switch typed := existing.(type) {
		case []any:
			result[key] = append(typed, value)
		default:
			result[key] = []any{typed, value}
		}
		return
	}
	result[key] = value
}

// PositionalNames returns the ordered positional argument names (snake_case)
// that params.Build maps for a route's ParamsGroup. It is the single
// authoritative source the parser itself uses, so CLI `--help` can enumerate a
// verb's positional arguments without drifting from real parsing behavior
// (issue #194). It returns nil for groups that accept no positionals.
func PositionalNames(group string) []string {
	return positionalNames(group)
}

func positionalNames(group string) []string {
	switch group {
	case "repo_add":
		return []string{"path"}
	case "repo_remove":
		return []string{"id"}
	case "daemon_token_create":
		// All inputs are flags (--capability, --display-name, ...); a bare
		// positional is treated as a capability name for convenience.
		return []string{"capability"}
	case "run_prepare":
		return []string{"workflow"}
	case "why":
		return []string{"target_id"}
	case "run_start", "run_pause", "run_resume", "run_cancel", "run_summary", "run_graph", "run_integrate":
		return []string{"run_id"}
	case "run_retry_job":
		return []string{"run_id", "job_id"}
	case "register_session":
		return []string{"run_id", "role", "lane"}
	case "session_close", "claim_next":
		return []string{"session_id"}
	case "work_claim_override":
		return []string{"session_id", "job_id", "decision_id"}
	case "work_packet_show":
		return []string{"packet_id"}
	case "ack", "release":
		return []string{"session_id", "message_id", "lease_id"}
	case "heartbeat":
		return []string{"session_id", "lease_id"}
	case "send":
		return []string{"session_id", "kind"}
	case "block":
		return []string{"session_id", "job_id", "lease_id"}
	case "complete":
		return []string{"session_id", "job_id", "lease_id"}
	case "publish_artifact":
		return []string{"session_id", "job_id", "lease_id", "kind", "logical_name", "path"}
	case "repo_write":
		return []string{"session_id", "job_id", "lease_id", "path"}
	case "repo_patch":
		return []string{"session_id", "job_id", "lease_id"}
	case "process_run":
		return []string{"session_id", "job_id", "lease_id"}
	case "verdict":
		return []string{"session_id", "job_id", "lease_id", "verdict"}
	case "submit_review":
		return []string{"session_id", "job_id", "lease_id", "path", "verdict"}
	case "override_verdict":
		return []string{"session_id", "job_id", "verdict"}
	case "recovery":
		return []string{"run_id"}
	case "recovery_resume":
		// #270: recovery.resume keys on blocker_id, not run_id. It shares the
		// "recovery" command group but needs its own positional mapping so
		// `striatum recovery resume <blocker-id>` reaches the daemon handler
		// (which requires blocker_id) instead of mapping the blocker to run_id.
		return []string{"blocker_id"}
	case "recovery_invalidate":
		return []string{"job_id", "decision_id"}
	case "recovery_reseal":
		// #271/#273: recovery.reseal keys on (run_id, job_id); it shares the
		// "recovery" command group but needs its own positional mapping so
		// `striatum recovery reseal <run-id> <job-id>` reaches the daemon
		// handler (the durability gate writes no blocker row to key on).
		return []string{"run_id", "job_id"}
	case "recovery_complete_stalled":
		// #292: recovery.complete_stalled keys on (run_id, job_id) so
		// `striatum recovery complete-stalled <run-id> <job-id>` finalizes a
		// recovery-exhausted job from its already-durable published artifacts.
		return []string{"run_id", "job_id"}
	case "recovery_resolve_blocker":
		// #304: recovery.resolve_blocker keys on blocker_id so
		// `striatum recovery resolve-blocker <blocker-id>` closes a dangling
		// non-escalation, non-checkpoint blocker that no completion path cleared.
		return []string{"blocker_id"}
	case "evidence_export", "corpus_export", "archive_create":
		return []string{"run_id", "out"}
	case "recall_search":
		return []string{"query"}
	case "decision_record":
		return []string{"run_id", "path", "outcome", "title"}
	case "checkpoint_resolve":
		return []string{"blocker_id", "action"}
	case "escalation_show", "escalation_resolve":
		return []string{"escalation_id"}
	case "branch_confirm":
		return []string{"run_id", "branch"}
	case "worktree_create":
		return []string{"session_id", "job_id", "lease_id"}
	case "worktree_release":
		return []string{"worktree_id"}
	case "worktree_anchor":
		return []string{"run_id", "job_id", "worktree_id"}
	case "supervise_start", "supervise_status", "supervise_stop", "supervise_rebridge", "supervise_trajectory":
		return []string{"session_id"}
	case "supervise_send":
		return []string{"session_id", "packet_id"}
	case "supervise_list":
		return []string{"run_id"}
	case "cross_repo":
		return []string{"cross_repo_run_id"}
	case "trajectory_export", "trajectory_watch":
		return []string{"run_id", "profile"}
	case "interrogation_open":
		return []string{"session_id", "target_session_id"}
	case "interrogation_ask", "interrogation_answer", "interrogation_close":
		return []string{"session_id", "interrogation_id"}
	case "interrogation_list":
		return []string{"run_id"}
	case "interrogation_show":
		return []string{"interrogation_id"}
	case "conversation_open":
		return []string{"participant_session_ids"}
	case "conversation_say":
		return []string{"session_id", "conversation_id", "body"}
	case "conversation_close":
		return []string{"session_id", "conversation_id"}
	case "conversation_list":
		return []string{"run_id"}
	case "conversation_show":
		return []string{"conversation_id"}
	default:
		return nil
	}
}

func applyAliases(group string, result map[string]any) {
	if value, ok := result["repo"].(string); ok && result["repository_id"] == nil {
		result["repository_id"] = value
	}
	if value, ok := result["workflow_path"].(string); ok && group == "run_prepare" && result["workflow"] == nil {
		result["workflow"] = value
	}
	if value, ok := result["out"].(string); ok && result["path"] == nil && (group == "evidence_export" || group == "corpus_export" || group == "archive_create") {
		result["path"] = value
	}
	if group == "daemon_token_create" {
		// Repeated --capability flags collapse into a []any under "capability";
		// the daemon.token.create handler expects multiple capabilities under
		// "capabilities". Move a multi-valued capability list across so
		// `--capability read --capability apply` issues a multi-grant token.
		if value, ok := result["capability"].([]any); ok && result["capabilities"] == nil {
			result["capabilities"] = value
			delete(result, "capability")
		}
	}
	if group == "supervise_trajectory" {
		if value, ok := result["tail"].(int); ok {
			result["tail_lines"] = value
			delete(result, "tail")
		}
	}
}
