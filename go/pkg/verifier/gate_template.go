package verifier

// RFC 0141 / D238 Pillar 3 (UNFILLED) — the pure, no-execution check that
// `workflow validate` and `run start` both hard-block on. An UNFILLED verification
// template must read RED before the run ever starts, never a false green: a
// generated gate that wires in an EXTERNAL check the operator has not yet pinned
// would otherwise mint nothing and report success.
//
// EvaluateAllowlistTemplate is authoritative AND self-updating: it reads the actual
// intent + per-host pins off disk, so once the operator runs `verifier pin
// --host-here` the block clears WITHOUT regenerating the workflow (the static
// allowlist_status hint can go stale; the files cannot). It executes NOTHING — it
// only reads sealed bytes, so it is safe on the daemon's gate path (D227).

import (
	"fmt"
	"path/filepath"
	"strings"
)

// TemplateBlock is a hard-block verdict: the verification template is unfilled.
type TemplateBlock struct {
	// Reason is a stable machine code (for rpc.Error content).
	Reason string
	// Message is the operator-facing explanation naming the offending entries.
	Message string
	// FixCommand is the literal command that resolves the block.
	FixCommand string
	// UnpinnedEntries are the sanctioned-but-unpinned check ids.
	UnpinnedEntries []string
	// IntentPath is the intent file the block was evaluated against (if resolved).
	IntentPath string
}

// AllowlistStatusUnfilled / Filled are the workflow.json hints the generator stamps.
const (
	AllowlistStatusUnfilled = "TEMPLATE_UNFILLED"
	AllowlistStatusFilled   = "FILLED"
)

// EvaluateAllowlistTemplate returns a non-nil TemplateBlock when the workflow is a
// verification gate whose external checks are sanctioned-but-unpinned on this host;
// nil when there is nothing to block (not a verification gate, builtins-only, or
// already pinned). repoRoot is the run's repo root; workflow is the parsed
// workflow.json.
func EvaluateAllowlistTemplate(repoRoot string, workflow map[string]any) *TemplateBlock {
	status := allowlistStatus(workflow)
	hasVerify := workflowHasVerifyJob(workflow)
	// Applicability: only verification gates are in scope. A workflow with no verify
	// job and no allowlist hint is untouched (normal workflows never block here).
	if !hasVerify && status == "" {
		return nil
	}
	// A gate the generator marked FILLED (or an unmarked builtins-only gate) has no
	// external checks wired into the gate — runnable and honest out of the box. We
	// TRUST the generator's FILLED determination and do NOT re-read the intent file
	// here: the intent template always ships one illustrative external check so it
	// parses, and that example is not part of a builtins-only gate. Re-deriving from
	// the template would wrongly block the runnable default.
	if status != AllowlistStatusUnfilled {
		return nil
	}
	// TEMPLATE_UNFILLED: authoritatively re-derive from the files so a post-pin run
	// clears WITHOUT regenerating the workflow (the static hint goes stale; the
	// files do not). The generator declares the real intent_path; fall back to the
	// repo-root convention only if it did not.
	intentPath, ok := declaredIntentPath(workflow)
	if !ok {
		intentPath = defaultIntentRepoPath
	}
	return blockIfUnpinned(repoRoot, intentPath)
}

const defaultIntentRepoPath = "verification/allowlist.intent.json"

// blockIfUnpinned loads the intent + per-host pins and returns a block iff a
// sanctioned external check is unpinned. A missing/unreadable intent under a
// TEMPLATE_UNFILLED hint fails closed with the fix command.
func blockIfUnpinned(repoRoot, intentRelOrAbs string) *TemplateBlock {
	intentPath := intentRelOrAbs
	if !filepath.IsAbs(intentPath) {
		intentPath = filepath.Join(repoRoot, intentRelOrAbs)
	}
	intent, err := LoadIntent(intentPath)
	if err != nil {
		// The sanctioned set cannot be read; do not pretend it is filled.
		return &TemplateBlock{
			Reason:     "verification_gate_unfilled",
			Message:    fmt.Sprintf("verification gate allowlist intent could not be read at %s (%v); the gate cannot be confirmed filled", intentPath, err),
			FixCommand: "striatum verifier pin --host-here",
			IntentPath: intentPath,
		}
	}
	pinsPath := filepath.Join(filepath.Dir(intentPath), PinsFileName(HostFingerprint()))
	var pins *Allowlist
	if loaded, perr := LoadAllowlist(pinsPath); perr == nil {
		pins = loaded
	}
	filled, unpinned := PinsAreFilled(intent, pins)
	if filled {
		return nil
	}
	pointers := make([]string, 0, len(unpinned))
	ids := intent.IDs()
	for _, id := range unpinned {
		idx := indexOf(ids, id)
		pointers = append(pointers, fmt.Sprintf("%s (intent#/checks/%d)", id, idx))
	}
	return &TemplateBlock{
		Reason: "verification_gate_unfilled",
		Message: fmt.Sprintf(
			"verification gate is UNFILLED: %d sanctioned check(s) have no pin on this host (%s): %s. The gate would mint no evidence and must not start; pin the bytes first (the sandbox observes them, you never type a sha): striatum verifier pin --host-here --intent %s",
			len(unpinned), HostFingerprint(), strings.Join(pointers, ", "), intentRelOrAbs,
		),
		FixCommand:      fmt.Sprintf("striatum verifier pin --host-here --intent %s", intentRelOrAbs),
		UnpinnedEntries: unpinned,
		IntentPath:      intentPath,
	}
}

// allowlistStatus reads the generator's allowlist_status hint from the top level or
// a nested `verification` object.
func allowlistStatus(workflow map[string]any) string {
	if s, ok := workflow["allowlist_status"].(string); ok {
		return strings.TrimSpace(s)
	}
	if v, ok := workflow["verification"].(map[string]any); ok {
		if s, ok := v["allowlist_status"].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// declaredIntentPath returns the intent path the workflow declares (top-level
// `intent_path` or `verification.intent_path`), ok=false if none.
func declaredIntentPath(workflow map[string]any) (string, bool) {
	if s, ok := workflow["intent_path"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s), true
	}
	if v, ok := workflow["verification"].(map[string]any); ok {
		if s, ok := v["intent_path"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s), true
		}
	}
	return "", false
}

// workflowHasVerifyJob reports whether any job declares type "verify".
func workflowHasVerifyJob(workflow map[string]any) bool {
	jobs, ok := workflow["jobs"]
	if !ok {
		return false
	}
	for _, j := range asMapList(jobs) {
		if t, ok := j["type"].(string); ok && strings.TrimSpace(t) == "verify" {
			return true
		}
	}
	return false
}

// asMapList normalizes a jobs value (either []any or []map[string]any) to maps.
func asMapList(v any) []map[string]any {
	switch list := v.(type) {
	case []map[string]any:
		return list
	case []any:
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func indexOf(s []string, want string) int {
	for i, v := range s {
		if v == want {
			return i
		}
	}
	return -1
}
