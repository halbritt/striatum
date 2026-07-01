package rpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type contractMethod struct {
	Method              string  `json:"method"`
	RequiredCapability  *string `json:"required_capability"`
	RepositoryScope     *bool   `json:"repository_scope"`
	RepositoryScopeMode string  `json:"repository_scope_mode"`
	ParamsSchemaVersion int     `json:"params_schema_version"`
	AuditClass          string  `json:"audit_class"`
	MinEnvelope         int     `json:"min_envelope"`
	Deprecated          bool    `json:"deprecated"`
}

type contractMethodsDocument struct {
	Methods []contractMethod `json:"methods"`
}

type routeBudgetContractDocument struct {
	RouteBudgetGuard routeBudgetGuard            `json:"route_budget_guard"`
	Methods          []routeBudgetContractMethod `json:"methods"`
}

type routeBudgetGuard struct {
	LegacyNoRouteBudgetMethodsCount  int    `json:"legacy_no_route_budget_methods_count"`
	LegacyNoRouteBudgetMethodsSHA256 string `json:"legacy_no_route_budget_methods_sha256"`
}

type routeBudgetContractMethod struct {
	Method      string               `json:"method"`
	Deprecated  bool                 `json:"deprecated"`
	RouteBudget *routeBudgetMetadata `json:"route_budget"`
}

type routeBudgetMetadata struct {
	Kind              string `json:"kind"`
	ReplacesMethod    string `json:"replaces_method"`
	ExtendsTransition string `json:"extends_transition"`
	Transition        string `json:"transition"`
	DecisionRef       string `json:"decision_ref"`
	CanonicalMethod   string `json:"canonical_method"`
	RetirementPlan    string `json:"retirement_plan"`
	Rationale         string `json:"rationale"`
}

func TestRegistryMatchesDaemonMethodsContract(t *testing.T) {
	contractPath := filepath.Join(findRepositoryRoot(t), "contracts", "daemon_methods.json")
	payload, err := os.ReadFile(contractPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("contracts/daemon_methods.json is not present in this checkout")
	}
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}

	expected := methodsByName(t, decodeContractMethods(t, payload))

	// Reconcile BOTH views of the registry against the contract:
	//
	//   1. The static-slice view (SortedMethods over methodEntries) is the
	//      generated/on-contract surface.
	//   2. The live-map view (the rpc.MethodRegistry map actually consulted by
	//      the authority guard at request time, rpc/server.go) can diverge from
	//      the static slice when a method is hand-registered into the map at
	//      runtime (`rpc.MethodRegistry[...] = rpc.NewMethod(...)`). Such a method
	//      is invisible to SortedMethods()/methodEntries, so a guard that only
	//      checked the static slice would PASS while the method was genuinely
	//      off-contract. supervise.rebridge was exactly that blind spot (#363).
	//
	// Within the rpc package the map is just buildRegistry(methodEntries), so
	// these two views are identical here. The cross-package case — a map-only
	// registration performed by the reads/mutations packages after they call
	// reads.Register/mutations.Register — is caught by the companion guard
	// TestLiveMethodRegistryMatchesDaemonMethodsContract in cmd/striatumd, which
	// reconciles the fully-populated live map against this same contract.
	for name, actual := range map[string]map[string]contractMethod{
		"static methodEntries slice (SortedMethods)": methodsByName(t, registryContractView()),
		"live rpc.MethodRegistry map":                methodsByName(t, registryMapContractView()),
	} {
		if missing, extra := mapKeyDiff(expected, actual); len(missing) > 0 || len(extra) > 0 {
			t.Fatalf("Go registry %s drifts from contract; missing=%v extra=%v", name, missing, extra)
		}
		for method, want := range expected {
			got := actual[method]
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: %s metadata drifts from contract:\n got: %#v\nwant: %#v", name, method, got, want)
			}
		}
	}
}

func TestMethodsETagMatchesDaemonMethodsContract(t *testing.T) {
	contractPath := filepath.Join(findRepositoryRoot(t), "contracts", "daemon_methods.json")
	payload, err := os.ReadFile(contractPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("contracts/daemon_methods.json is not present in this checkout")
	}
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}

	expected := methodsETagFromContract(t, decodeContractMethods(t, payload))
	if got := MethodsETag(); got != expected {
		t.Fatalf("MethodsETag() = %s, want %s", got, expected)
	}
}

func TestDaemonMethodsContractRouteBudgetGate(t *testing.T) {
	contractPath := filepath.Join(findRepositoryRoot(t), "contracts", "daemon_methods.json")
	payload, err := os.ReadFile(contractPath)
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("contracts/daemon_methods.json is not present in this checkout")
	}
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}

	var document routeBudgetContractDocument
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("decode daemon method contract route-budget metadata: %v", err)
	}
	if document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsCount == 0 {
		t.Fatal("route_budget_guard.legacy_no_route_budget_methods_count is required")
	}
	if !strings.HasPrefix(document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsSHA256, "sha256:") {
		t.Fatalf("route_budget_guard.legacy_no_route_budget_methods_sha256 = %q, want sha256:<hex>", document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsSHA256)
	}

	legacyWithoutBudget := make([]string, 0)
	for _, method := range document.Methods {
		if method.RouteBudget == nil {
			if method.Deprecated {
				t.Errorf("deprecated method %s must declare route_budget.kind=deprecated_alias with a retirement plan", method.Method)
				continue
			}
			legacyWithoutBudget = append(legacyWithoutBudget, method.Method)
			continue
		}
		validateRouteBudgetMetadata(t, method)
	}

	legacyHash := routeBudgetMethodSetSHA256(t, legacyWithoutBudget)
	if len(legacyWithoutBudget) != document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsCount ||
		legacyHash != document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsSHA256 {
		t.Fatalf("non-deprecated methods without route_budget changed; add route_budget metadata to new methods or update the grandfathered baseline with review evidence. got count=%d sha=%s want count=%d sha=%s",
			len(legacyWithoutBudget),
			legacyHash,
			document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsCount,
			document.RouteBudgetGuard.LegacyNoRouteBudgetMethodsSHA256,
		)
	}
}

func decodeContractMethods(t *testing.T, payload []byte) []contractMethod {
	t.Helper()
	var document contractMethodsDocument
	if err := json.Unmarshal(payload, &document); err == nil && document.Methods != nil {
		return normalizeContractMethods(document.Methods)
	}

	var methods []contractMethod
	if err := json.Unmarshal(payload, &methods); err != nil {
		t.Fatalf("decode daemon method contract: %v", err)
	}
	return normalizeContractMethods(methods)
}

func normalizeContractMethods(methods []contractMethod) []contractMethod {
	normalized := make([]contractMethod, 0, len(methods))
	for _, method := range methods {
		if method.RepositoryScopeMode == "" && method.RepositoryScope == nil {
			method.RepositoryScopeMode = string(ScopeDaemonGlobal)
		}
		if method.RepositoryScopeMode == "" {
			if *method.RepositoryScope {
				method.RepositoryScopeMode = string(ScopeSingleRepo)
			} else {
				method.RepositoryScopeMode = string(ScopeDaemonGlobal)
			}
		}
		if method.RepositoryScope == nil {
			repositoryScope := method.RepositoryScopeMode == string(ScopeSingleRepo)
			method.RepositoryScope = &repositoryScope
		}
		if method.ParamsSchemaVersion == 0 {
			method.ParamsSchemaVersion = 1
		}
		if method.AuditClass == "" {
			method.AuditClass = "metadata"
		}
		if method.MinEnvelope == 0 {
			method.MinEnvelope = 1
		}
		normalized = append(normalized, method)
	}
	return normalized
}

func validateRouteBudgetMetadata(t *testing.T, method routeBudgetContractMethod) {
	t.Helper()
	budget := method.RouteBudget
	kind := strings.TrimSpace(budget.Kind)
	if kind == "" {
		t.Fatalf("%s route_budget.kind is required", method.Method)
	}
	switch kind {
	case "replaces_existing_method":
		requireRouteBudgetField(t, method.Method, "replaces_method", budget.ReplacesMethod)
		requireRouteBudgetField(t, method.Method, "decision_ref", budget.DecisionRef)
		requireRouteBudgetField(t, method.Method, "rationale", budget.Rationale)
	case "extends_existing_transition":
		requireRouteBudgetField(t, method.Method, "extends_transition", budget.ExtendsTransition)
		requireRouteBudgetField(t, method.Method, "decision_ref", budget.DecisionRef)
		requireRouteBudgetField(t, method.Method, "rationale", budget.Rationale)
	case "new_daemon_transition":
		requireRouteBudgetField(t, method.Method, "transition", budget.Transition)
		requireRouteBudgetField(t, method.Method, "decision_ref", budget.DecisionRef)
		requireRouteBudgetField(t, method.Method, "rationale", budget.Rationale)
	case "one_shot_backfill":
		requireRouteBudgetField(t, method.Method, "transition", budget.Transition)
		requireRouteBudgetField(t, method.Method, "decision_ref", budget.DecisionRef)
		requireRouteBudgetField(t, method.Method, "retirement_plan", budget.RetirementPlan)
		requireRouteBudgetField(t, method.Method, "rationale", budget.Rationale)
	case "deprecated_alias":
		if !method.Deprecated {
			t.Fatalf("%s declares route_budget.kind=deprecated_alias but deprecated=false", method.Method)
		}
		requireRouteBudgetField(t, method.Method, "canonical_method", budget.CanonicalMethod)
		requireRouteBudgetField(t, method.Method, "retirement_plan", budget.RetirementPlan)
	default:
		t.Fatalf("%s route_budget.kind = %q, want replaces_existing_method, extends_existing_transition, new_daemon_transition, one_shot_backfill, or deprecated_alias", method.Method, kind)
	}
	if method.Deprecated && kind != "deprecated_alias" {
		t.Fatalf("%s is deprecated and must use route_budget.kind=deprecated_alias", method.Method)
	}
}

func requireRouteBudgetField(t *testing.T, method, field, value string) {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s route_budget.%s is required", method, field)
	}
}

func routeBudgetMethodSetSHA256(t *testing.T, methods []string) string {
	t.Helper()
	sort.Strings(methods)
	payload, err := json.Marshal(methods)
	if err != nil {
		t.Fatalf("marshal route-budget method set: %v", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func registryContractView() []contractMethod {
	return contractViewOf(SortedMethods())
}

// registryMapContractView projects the LIVE rpc.MethodRegistry map (the one the
// authority guard consults at request time), not the static methodEntries slice.
// A method hand-registered into the map at runtime is absent from SortedMethods()
// but present here, so reconciling this view against the contract catches
// map-only registrations that would otherwise be invisible to the guard (#363).
func registryMapContractView() []contractMethod {
	entries := make([]MethodEntry, 0, len(MethodRegistry))
	for _, entry := range MethodRegistry {
		entries = append(entries, entry)
	}
	return contractViewOf(entries)
}

func contractViewOf(entries []MethodEntry) []contractMethod {
	methods := make([]contractMethod, 0, len(entries))
	for _, entry := range entries {
		var capability *string
		if entry.RequiredCapability != nil {
			value := string(*entry.RequiredCapability)
			capability = &value
		}
		repositoryScope := entry.RepositoryScope
		methods = append(methods, contractMethod{
			Method:              entry.Method,
			RequiredCapability:  capability,
			RepositoryScope:     &repositoryScope,
			RepositoryScopeMode: string(entry.RepositoryScopeMode),
			ParamsSchemaVersion: entry.ParamsSchemaVersion,
			AuditClass:          entry.AuditClass,
			MinEnvelope:         entry.MinEnvelope,
			Deprecated:          entry.Deprecated,
		})
	}
	return methods
}

func methodsETagFromContract(t *testing.T, methods []contractMethod) string {
	t.Helper()
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].Method < methods[j].Method
	})
	material := make([]map[string]any, 0, len(methods))
	for _, method := range methods {
		material = append(material, contractMethodPublicMap(method))
	}
	payload, err := json.Marshal(material)
	if err != nil {
		t.Fatalf("marshal contract etag material: %v", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func contractMethodPublicMap(method contractMethod) map[string]any {
	var requiredCapability any
	if method.RequiredCapability != nil {
		requiredCapability = *method.RequiredCapability
	}
	repositoryScope := false
	if method.RepositoryScope != nil {
		repositoryScope = *method.RepositoryScope
	}
	return map[string]any{
		"method":                method.Method,
		"required_capability":   requiredCapability,
		"repository_scope":      repositoryScope,
		"repository_scope_mode": method.RepositoryScopeMode,
		"params_schema_version": method.ParamsSchemaVersion,
		"audit_class":           method.AuditClass,
		"min_envelope":          method.MinEnvelope,
		"deprecated":            method.Deprecated,
	}
}

func methodsByName(t *testing.T, methods []contractMethod) map[string]contractMethod {
	t.Helper()
	byName := make(map[string]contractMethod, len(methods))
	for _, method := range methods {
		if method.Method == "" {
			t.Fatalf("contract contains method without a name")
		}
		if _, exists := byName[method.Method]; exists {
			t.Fatalf("contract contains duplicate method %s", method.Method)
		}
		byName[method.Method] = method
	}
	return byName
}

func mapKeyDiff(expected map[string]contractMethod, actual map[string]contractMethod) ([]string, []string) {
	missing := make([]string, 0)
	extra := make([]string, 0)
	for key := range expected {
		if _, ok := actual[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	return missing, extra
}

func findRepositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go", "go.mod")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && filepath.Base(dir) == "go" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root from %s", dir)
		}
		dir = parent
	}
}
