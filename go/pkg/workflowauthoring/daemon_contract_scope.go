package workflowauthoring

import (
	"fmt"
	"regexp"
	"strings"
)

const daemonContractPath = "contracts/daemon_methods.json"

var capabilityNamePattern = regexp.MustCompile(`\bCapability[A-Z][A-Za-z0-9_]*\b`)

// DaemonContractScopeWarnings reports repo-write jobs whose authoring text points
// at daemon contract surfaces but whose write scope cannot update the generated
// daemon method contract.
func DaemonContractScopeWarnings(workflow map[string]any) []map[string]any {
	warnings := []map[string]any{}
	for _, entry := range crossRepoJobEntries(workflow) {
		if !jobIsRepoWrite(entry.job) {
			continue
		}
		scope, _ := entry.job["write_scope"].(map[string]any)
		allowed := stringsFromSlice(scope["allowed_paths"])
		if writeScopeAllowsDaemonContract(allowed) {
			continue
		}
		for _, field := range promptFreeTextFields(entry.job) {
			trigger, ok := daemonContractTrigger(field.value)
			if !ok {
				continue
			}
			warnings = append(warnings, map[string]any{
				"rule":          "daemon_contract_scope_missing",
				"severity":      "warning",
				"job_id":        entry.id,
				"field":         field.name,
				"trigger":       trigger,
				"required_path": daemonContractPath,
				"allowed_paths": allowed,
				"message":       DaemonContractScopeMessage(entry.id, field.name, trigger),
			})
			break
		}
	}
	return warnings
}

func DaemonContractScopeMessage(jobID, field, trigger string) string {
	return fmt.Sprintf(
		"repo-write job %q prompt field %s mentions daemon contract surface %q, but write_scope.allowed_paths does not include %s or contracts/; add the contract scope or split the daemon contract change into a separate workflow before running",
		jobID, field, trigger, daemonContractPath,
	)
}

func writeScopeAllowsDaemonContract(allowed []string) bool {
	for _, allowedPath := range allowed {
		if repoPathWithin(daemonContractPath, allowedPath) {
			return true
		}
	}
	return false
}

func daemonContractTrigger(text string) (string, bool) {
	if match := capabilityNamePattern.FindString(text); match != "" {
		return match, true
	}
	lower := strings.ToLower(text)
	triggers := []string{
		"contracts/daemon_methods.json",
		"daemon_methods.json",
		"daemon method",
		"daemon-method",
		"daemon method table",
		"generated daemon method table",
		"command-authority-matrix",
		"command authority matrix",
		"rpc capability",
		"methodentry",
	}
	for _, trigger := range triggers {
		if strings.Contains(lower, trigger) {
			return trigger, true
		}
	}
	if (strings.Contains(lower, "rpc") || strings.Contains(lower, "daemon")) && strings.Contains(lower, "route") {
		return "daemon/rpc route", true
	}
	if strings.Contains(lower, "registry") && (strings.Contains(lower, "daemon") || strings.Contains(lower, "method") || strings.Contains(lower, "route") || strings.Contains(lower, "contract")) {
		return "daemon registry", true
	}
	return "", false
}
