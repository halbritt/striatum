//go:generate go run ../cli/routergen -mode rpc-registry -contract ../../../contracts/daemon_methods.json -out registry_methods.go
//go:generate go run ../cli/routergen -mode markdown-tables -contract ../../../contracts/daemon_methods.json -out ../../../docs/reference/daemon-method-tables.md

package rpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type Capability string

const (
	CapabilityRead             Capability = "read"
	CapabilityWrite            Capability = "write"
	CapabilityReview           Capability = "review"
	CapabilityClaim            Capability = "claim"
	CapabilityApply            Capability = "apply"
	CapabilityAdmin            Capability = "admin"
	CapabilityRecovery         Capability = "recovery"
	CapabilitySurgicalRecovery Capability = "surgical_recovery"
	// CapabilityReseal is daemon-internal recovery authority for RFC 0143 Slice B.
	// It is deliberately not present in Capabilities, so no bearer token can be
	// minted with general-purpose reseal authority through the public registry.
	CapabilityReseal Capability = "reseal"
)

var Capabilities = map[Capability]struct{}{
	CapabilityRead:             {},
	CapabilityWrite:            {},
	CapabilityReview:           {},
	CapabilityClaim:            {},
	CapabilityApply:            {},
	CapabilityAdmin:            {},
	CapabilityRecovery:         {},
	CapabilitySurgicalRecovery: {},
}

type ScopeMode string

const (
	ScopeSingleRepo   ScopeMode = "single_repo"
	ScopeDaemonGlobal ScopeMode = "daemon_global"
)

type MethodEntry struct {
	Method              string      `json:"method"`
	RequiredCapability  *Capability `json:"required_capability"`
	RepositoryScope     bool        `json:"repository_scope"`
	RepositoryScopeMode ScopeMode   `json:"repository_scope_mode"`
	ParamsSchemaVersion int         `json:"params_schema_version"`
	AuditClass          string      `json:"audit_class"`
	MinEnvelope         int         `json:"min_envelope"`
	Deprecated          bool        `json:"deprecated"`
}

func NewMethod(method string, capability *Capability, repositoryScope bool, mode ScopeMode) MethodEntry {
	if mode == "" {
		if repositoryScope {
			mode = ScopeSingleRepo
		} else {
			mode = ScopeDaemonGlobal
		}
	}
	return MethodEntry{
		Method:              method,
		RequiredCapability:  capability,
		RepositoryScope:     repositoryScope,
		RepositoryScopeMode: mode,
		ParamsSchemaVersion: 1,
		AuditClass:          "metadata",
		MinEnvelope:         1,
	}
}

func CapPtr(capability Capability) *Capability {
	return &capability
}

func DeprecatedAlias(method string, capability Capability) MethodEntry {
	entry := NewMethod(method, CapPtr(capability), true, "")
	entry.Deprecated = true
	return entry
}

var MethodRegistry = buildRegistry(methodEntries)

func buildRegistry(entries []MethodEntry) map[string]MethodEntry {
	registry := make(map[string]MethodEntry, len(entries))
	for _, entry := range entries {
		registry[entry.Method] = entry
	}
	return registry
}

func DescribeMethods() map[string]any {
	entries := SortedMethods()
	return map[string]any{
		"methods_etag": MethodsETag(),
		"methods":      entries,
	}
}

func SortedMethods() []MethodEntry {
	entries := append([]MethodEntry(nil), methodEntries...)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Method < entries[j].Method
	})
	return entries
}

func MethodsETag() string {
	entries := SortedMethods()
	material := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		material = append(material, methodEntryPublicMap(entry))
	}
	payload, _ := json.Marshal(material)
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func methodEntryPublicMap(entry MethodEntry) map[string]any {
	return map[string]any{
		"method":                entry.Method,
		"required_capability":   entry.RequiredCapability,
		"repository_scope":      entry.RepositoryScope,
		"repository_scope_mode": entry.RepositoryScopeMode,
		"params_schema_version": entry.ParamsSchemaVersion,
		"audit_class":           entry.AuditClass,
		"min_envelope":          entry.MinEnvelope,
		"deprecated":            entry.Deprecated,
	}
}
