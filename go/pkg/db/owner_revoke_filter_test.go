package db

import (
	"context"
	"os"
	"strings"
	"testing"
)

// recordingTx records every SQL string executed in the transaction so a test can
// assert which owner bundles reached applyOneOwnerBundle (which Exec's the bundle
// SQL). It is a canned fake (no real DB) for the M2 synthetic-phase F16a test.
type recordingTx struct{ execed *[]string }

func (t *recordingTx) Exec(_ context.Context, sql string, _ ...any) error {
	*t.execed = append(*t.execed, sql)
	return nil
}
func (t *recordingTx) QueryRow(context.Context, string, ...any) Row { return nil }
func (t *recordingTx) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", nil
}
func (t *recordingTx) Commit(context.Context) error   { return nil }
func (t *recordingTx) Rollback(context.Context) error { return nil }

type recordingRunner struct{ execed []string }

func (r *recordingRunner) Exec(context.Context, string, ...any) error   { return nil }
func (r *recordingRunner) QueryRow(context.Context, string, ...any) Row { return nil }
func (r *recordingRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "0", nil
}
func (r *recordingRunner) BeginTx(context.Context) (TxRunner, error) {
	return &recordingTx{execed: &r.execed}, nil
}

const syntheticRevokeMarker = "-- SYNTHETIC 0021 REVOKE CREATE (must never be owner-ddl applied)"

// TestOwnerDDLApplyExcludesSyntheticRevokeBundle is the M2 F16a synthetic-phase
// test (PROPOSAL §3.2a / §5 F16a). It drives the non-revoke filter through a
// HAND-BUILT bundle list containing a synthetic {Version: 21} entry, WITHOUT
// asserting that production OwnerBundles() contains 0021 (it does not until the
// build's step 7). It proves the filter + both apply loops + the nil-fallback all
// exclude the DDL-revoke bundle.
func TestOwnerDDLApplyExcludesSyntheticRevokeBundle(t *testing.T) {
	ctx := context.Background()

	// (predicate) isNonRevokeBundle excludes exactly the revoke frontier.
	if !isNonRevokeBundle(LatestOwnerBundleVersion) {
		t.Fatalf("isNonRevokeBundle(%d) = false; the latest non-revoke bundle must be apply-eligible", LatestOwnerBundleVersion)
	}
	if isNonRevokeBundle(DDLRevokeOwnerBundleVersion) {
		t.Fatalf("isNonRevokeBundle(%d) = true; the DDL-revoke bundle must be excluded", DDLRevokeOwnerBundleVersion)
	}

	// (a) OwnerDDLApplyBundles never surfaces a bundle >= the revoke frontier.
	applyEligible, err := OwnerDDLApplyBundles()
	if err != nil {
		t.Fatalf("OwnerDDLApplyBundles: %v", err)
	}
	for _, b := range applyEligible {
		// Exact-version exclusion (RFC 0167 P0): normal bundles above the revoke
		// (e.g. 0022) ARE apply-eligible; only the revoke (0021) is excluded.
		if b.Version == DDLRevokeOwnerBundleVersion {
			t.Fatalf("OwnerDDLApplyBundles surfaced the DDL-revoke bundle %d; it must be filtered out", b.Version)
		}
	}

	synthetic := []OwnerBundle{
		{Version: 18, Label: "synthetic non-revoke", SQL: "-- v18 non-revoke"},
		{Version: 20, Label: "synthetic non-revoke", SQL: "-- v20 non-revoke"},
		{Version: DDLRevokeOwnerBundleVersion, Label: "synthetic revoke", SQL: syntheticRevokeMarker},
	}

	// (b1) applyPendingOwnerBundles skips the hand-passed synthetic 0021.
	pending := &recordingRunner{}
	if _, _, err := applyPendingOwnerBundles(ctx, pending, synthetic, 17, "synthetic"); err != nil {
		t.Fatalf("applyPendingOwnerBundles: %v", err)
	}
	assertRevokeNotApplied(t, "applyPendingOwnerBundles", pending.execed)
	if !containsExec(pending.execed, "-- v18 non-revoke") || !containsExec(pending.execed, "-- v20 non-revoke") {
		t.Fatalf("applyPendingOwnerBundles skipped a non-revoke bundle it should have applied: %v", pending.execed)
	}

	// (b2) ReapplyAllOwnerBundles skips the hand-passed synthetic 0021.
	reapply := &recordingRunner{}
	if _, err := ReapplyAllOwnerBundles(ctx, reapply, synthetic, "synthetic"); err != nil {
		t.Fatalf("ReapplyAllOwnerBundles: %v", err)
	}
	assertRevokeNotApplied(t, "ReapplyAllOwnerBundles", reapply.execed)

	// (c) ReapplyAllOwnerBundles(nil, …) resolves its fallback to the filtered
	// loader: no bundle >= the revoke frontier reaches applyOneOwnerBundle.
	fallback := &recordingRunner{}
	if _, err := ReapplyAllOwnerBundles(ctx, fallback, nil, "synthetic"); err != nil {
		t.Fatalf("ReapplyAllOwnerBundles(nil): %v", err)
	}
	assertRevokeNotApplied(t, "ReapplyAllOwnerBundles(nil-fallback)", fallback.execed)
}

func assertRevokeNotApplied(t *testing.T, route string, execed []string) {
	t.Helper()
	if containsExec(execed, syntheticRevokeMarker) {
		t.Fatalf("%s applied the synthetic DDL-revoke bundle; M2 requires it be excluded from every owner-ddl apply route", route)
	}
}

func containsExec(execed []string, marker string) bool {
	for _, sql := range execed {
		if strings.Contains(sql, marker) {
			return true
		}
	}
	return false
}

// TestOwnerBundle0021StagedForActivationNotEmbedded is the F16b arm RE-SCOPED for
// the build's Option B (D7'): the DDL-revoke (0021) is STAGED outside ownerBundleFS,
// not embedded. For THIS inert-landing binary the shadow-first invariant holds —
// OwnerBundles() does NOT list 0021, RevokeBundleEmbedded() is FALSE, BuildPlan emits
// NO revoke step — while the staged dir still version-controls the revoke SQL and
// makes it loadable for the activation/verify binary's plan (§4.3). The production
// EMBED assertion + the forced FMA-007 self-heal pgtest are re-scoped to that
// activation binary (where RevokeBundleEmbedded() flips true), per the SEED's
// Option-B re-scope. This is the dormant-production-assertion trade-off the reviewer
// accepted, made explicit and tested in the un-embedded direction.
func TestOwnerBundle0021StagedForActivationNotEmbedded(t *testing.T) {
	// (a) production OwnerBundles() does NOT list the revoke (it is staged out).
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	for i := range bundles {
		if bundles[i].Version == DDLRevokeOwnerBundleVersion {
			t.Fatalf("OwnerBundles() lists bundle %d (the DDL-revoke); Option B requires it staged OUT of ownerBundleFS so RevokeBundleEmbedded() stays false", bundles[i].Version)
		}
	}

	// (b) RevokeBundleEmbedded() is FALSE — the load-bearing shadow-first invariant
	// (a none-cursor flag-OFF boot serves legacy instead of halting awaiting_deploy_config).
	embedded, err := RevokeBundleEmbedded()
	if err != nil {
		t.Fatalf("RevokeBundleEmbedded: %v", err)
	}
	if embedded {
		t.Fatal("RevokeBundleEmbedded() = true for the inert-landing build; Option B requires the revoke staged out of ownerBundleFS (D7')")
	}

	// (c) the staged dir DOES carry the revoke SQL (loadable for the activation plan):
	// StagedRevokeBundle resolves it, with a non-empty content hash and the REVOKE DDL.
	staged, ok, err := StagedRevokeBundle()
	if err != nil {
		t.Fatalf("StagedRevokeBundle: %v", err)
	}
	if !ok {
		t.Fatal("StagedRevokeBundle() found no staged revoke; the deployer must be able to load 0021 from sql/owner_staged_activation/")
	}
	if staged.Version != DDLRevokeOwnerBundleVersion {
		t.Fatalf("staged revoke version=%d, want %d", staged.Version, DDLRevokeOwnerBundleVersion)
	}
	if staged.SHA256() == "" {
		t.Fatal("staged DDL-revoke bundle has an empty SHA256")
	}
	if !strings.Contains(staged.SQL, "REVOKE CREATE ON SCHEMA striatumd FROM striatumd_rw") {
		t.Fatalf("staged DDL-revoke bundle SQL does not revoke CREATE from striatumd_rw:\n%s", staged.SQL)
	}

	// (d) OwnerDDLApplyBundles() excludes any revoke frontier (the listing split holds
	// for both the inert binary — trivially, none present — and an activation binary).
	apply, err := OwnerDDLApplyBundles()
	if err != nil {
		t.Fatalf("OwnerDDLApplyBundles: %v", err)
	}
	for _, b := range apply {
		if b.Version == DDLRevokeOwnerBundleVersion {
			t.Fatalf("OwnerDDLApplyBundles() surfaced the DDL-revoke bundle %d; the listing split is broken", b.Version)
		}
	}

	// (e) the watermark frontier is 24/24 (normal bundles 0022+ sit ABOVE
	// the staged revoke 0021); the revoke is still deploy-plan-terminal, NOT a
	// watermark advance, so the highest EMBEDDED owner bundle is 24 (21 is staged out).
	if LatestOwnerBundleVersion != 24 || RequiredOwnerBundleVersion != 24 {
		t.Fatalf("watermark frontier = Latest=%d Required=%d, want 24/24", LatestOwnerBundleVersion, RequiredOwnerBundleVersion)
	}
	if got := maxEmbeddedOwnerVersion(bundles); got != 24 {
		t.Fatalf("highest EMBEDDED owner bundle = %d, want 24 (the revoke 0021 is staged out; normal bundles above it are embedded)", got)
	}

	// (f) BuildPlan emits NO revoke step for the inert-landing binary (revoke-in-plan
	// re-scoped to the activation binary, §4.3): RevokeStepIndex == -1.
	plan, err := BuildPlan(0, 0)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.RevokeStepIndex != -1 {
		t.Fatalf("BuildPlan RevokeStepIndex=%d for the inert-landing binary, want -1 (no terminal revoke; re-scoped to the activation binary)", plan.RevokeStepIndex)
	}
}

// TestOwnerDDLApplyRoutesUseFilteredLoader is the M4 build-time grep guard: the
// only `OwnerBundles()` (full-loader) call sites inside owner.go are the
// non-apply consumers OwnerDDLApplyBundles and RevokeBundleEmbedded. Every
// `owner-ddl apply` route must load the filtered OwnerDDLApplyBundles() so the
// DDL-revoke bundle cannot be committed through the apply path.
func TestOwnerDDLApplyRoutesUseFilteredLoader(t *testing.T) {
	body, err := os.ReadFile("owner.go")
	if err != nil {
		t.Fatalf("read owner.go: %v", err)
	}
	allowedFullLoaderFuncs := map[string]bool{
		"OwnerDDLApplyBundles": true,
		"RevokeBundleEmbedded": true,
	}
	currentFunc := ""
	for _, line := range strings.Split(string(body), "\n") {
		if name := funcNameOf(line); name != "" {
			currentFunc = name
			continue // the declaration line itself is not a call site
		}
		// Only code lines count: a doc comment that merely mentions the loader is
		// not a call site.
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if strings.Contains(line, "OwnerBundles()") {
			if !allowedFullLoaderFuncs[currentFunc] {
				t.Fatalf("owner.go func %q calls the full OwnerBundles() loader; an owner-ddl apply route must call OwnerDDLApplyBundles() so the DDL-revoke bundle stays excluded (M2/M4):\n  %s", currentFunc, strings.TrimSpace(line))
			}
		}
	}
}

// funcNameOf extracts the function name from a Go top-level `func Name(` or
// `func (recv) Name(` declaration line, or "" when the line is not a declaration.
func funcNameOf(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "func ") {
		return ""
	}
	rest := strings.TrimPrefix(trimmed, "func ")
	if strings.HasPrefix(rest, "(") { // method: skip the receiver
		if idx := strings.Index(rest, ")"); idx >= 0 {
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}
	if idx := strings.IndexAny(rest, "("); idx >= 0 {
		return strings.TrimSpace(rest[:idx])
	}
	return ""
}
