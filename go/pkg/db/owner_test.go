package db

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/sessionliveness"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestIsCrossBundleDependencyError covers the FMA-007 (#458) trigger detection:
// a missing-object SQLSTATE a re-applied later bundle raises when an earlier
// bundle's object is gone is recognized; an unrelated error (or a non-pg error)
// is not, so the ordered self-heal only fires on the dependency class.
func TestIsCrossBundleDependencyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"undefined_table", &pgconn.PgError{Code: "42P01", Message: `relation "striatumd.schema_authority" does not exist`}, true},
		{"undefined_column", &pgconn.PgError{Code: "42703", Message: `column "x" does not exist`}, true},
		{"undefined_function", &pgconn.PgError{Code: "42883", Message: `function striatumd.foo() does not exist`}, true},
		{"undefined_object", &pgconn.PgError{Code: "42704", Message: `type "x" does not exist`}, true},
		{"wrapped_undefined_table", fmt.Errorf("apply: %w", &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}), true},
		{"insufficient_privilege", &pgconn.PgError{Code: "42501", Message: "permission denied"}, false},
		{"unique_violation", &pgconn.PgError{Code: "23505", Message: "duplicate key"}, false},
		{"non_pg_error", errors.New("boom"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isCrossBundleDependencyError(tc.err); got != tc.want {
				t.Fatalf("isCrossBundleDependencyError(%v) = %v; want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestWrapOwnerBundleApplyErrorIsLegible covers FMA-007 (#458): a missing
// cross-bundle object produces an actionable message naming the failing bundle,
// the missing object, and the one-step remediation — not a raw, opaque
// `relation does not exist`. The original error is preserved for errors.As, so
// the auto-reconcile trigger still recognizes it. An unrelated error keeps the
// existing wrapping.
func TestWrapOwnerBundleApplyErrorIsLegible(t *testing.T) {
	bundle := OwnerBundle{Version: 2, Label: "runtime read grant"}

	missing := &pgconn.PgError{Code: "42P01", Message: `relation "striatumd.schema_authority" does not exist`}
	wrapped := wrapOwnerBundleApplyError(bundle, missing)
	for _, needle := range []string{
		"owner bundle 2 (runtime read grant)",
		"missing cross-bundle dependency",
		"42P01",
		"striatumd.schema_authority",
		"striatum daemon owner-ddl apply",
		"database owner",
	} {
		if !strings.Contains(wrapped.Error(), needle) {
			t.Fatalf("legible error missing %q; got: %s", needle, wrapped.Error())
		}
	}
	// errors.As must still reach the original pg error so the self-heal trigger
	// and any caller-side classification keep working.
	var pgErr *pgconn.PgError
	if !errors.As(wrapped, &pgErr) || pgErr.Code != "42P01" {
		t.Fatalf("wrapped error lost the underlying pg error: %v", wrapped)
	}
	if !isCrossBundleDependencyError(wrapped) {
		t.Fatal("wrapped legible error must still classify as a cross-bundle dependency error")
	}

	// A non-dependency error keeps the plain wrapping (no remediation noise).
	other := wrapOwnerBundleApplyError(bundle, errors.New("connection reset"))
	if !strings.Contains(other.Error(), "apply owner bundle 2 (runtime read grant)") {
		t.Fatalf("non-dependency error lost its bundle context: %s", other.Error())
	}
	if strings.Contains(other.Error(), "missing cross-bundle dependency") {
		t.Fatalf("non-dependency error must not carry the cross-bundle remediation: %s", other.Error())
	}
}

func TestOwnerBundleSevenAddsArtifactPlacement(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 7 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 7 is missing")
	}
	for _, needle := range []string{
		"ALTER TABLE striatumd.artifacts",
		"ADD COLUMN IF NOT EXISTS placement text",
		"artifacts_placement_check",
		"p_placement text",
		"NULLIF(p_placement, '')",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 7 missing %q", needle)
		}
	}
}

func TestOwnerBundleEightAddsArtifactSearch(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 8 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 8 is missing")
	}
	for _, needle := range []string{
		"ADD COLUMN IF NOT EXISTS search_vector tsvector",
		"GENERATED ALWAYS AS",
		"to_tsvector('english'",
		"CREATE INDEX IF NOT EXISTS idx_artifacts_search",
		"USING GIN (search_vector)",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 8 missing %q", needle)
		}
	}
}

func TestOwnerBundleNineAddsReviewGeneration(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 9 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 9 is missing")
	}
	for _, needle := range []string{
		"ALTER TABLE striatumd.jobs",
		"ALTER TABLE striatumd.verdicts",
		"ADD COLUMN IF NOT EXISTS review_generation integer NOT NULL DEFAULT 1",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 9 missing %q", needle)
		}
	}
}

// TestOwnerBundleTenAddsWedgeStallClass is GH #324: the wedged_no_tool_progress
// liveness stall class is added to the sessions_liveness_stall_class_check CHECK.
// striatumd.sessions is an owner-held table, so widening the CHECK (DROP + re-ADD,
// which a CHECK cannot do in place) is owner-table DDL and MUST live in an owner
// bundle, never a runtime migration (RFC 0079 §5 / RFC 0081 owner-held ALTER
// crash-loop). The bundle must be idempotent so a re-run is a safe no-op.
func TestOwnerBundleTenAddsWedgeStallClass(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 10 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 10 is missing")
	}
	for _, needle := range []string{
		"sessions_liveness_stall_class_check",
		"DROP CONSTRAINT sessions_liveness_stall_class_check",
		"ADD CONSTRAINT sessions_liveness_stall_class_check",
		"'wedged_no_tool_progress'",
		"NOT LIKE '%wedged_no_tool_progress%'",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 10 missing %q", needle)
		}
	}
	// Ownership-safety: it ALTERs the owner-held sessions table (that is the whole
	// point of the bundle), and it must NOT touch any other owner table.
	if !strings.Contains(bundle.SQL, "ALTER TABLE striatumd.sessions") {
		t.Fatal("bundle 10 must ALTER striatumd.sessions")
	}
}

// TestOwnerBundleTenWedgeClassMatchesClassifier keeps the persisted CHECK list in
// lockstep with the classifier constant — if sessionliveness renames the class,
// this fails until the bundle (and a new bundle version) follows.
func TestOwnerBundleTenWedgeClassMatchesClassifier(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 10 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 10 is missing")
	}
	if !strings.Contains(bundle.SQL, "'"+sessionliveness.StallToolProgress+"'") {
		t.Fatalf("bundle 10 CHECK must permit the classifier stall class %q", sessionliveness.StallToolProgress)
	}
}

// TestOwnerBundleTwelveAddsQuarantineState is GH #311 P0: the 'quarantined' job
// state is added to the jobs_state_check CHECK. striatumd.jobs is an owner-held
// table, so widening the CHECK (DROP + re-ADD, which a CHECK cannot do in place)
// is owner-table DDL and MUST live in an owner bundle, never a runtime migration
// (RFC 0079 §5 / RFC 0081 owner-held ALTER crash-loop). The bundle must be
// idempotent so a re-run is a safe no-op. Bundle 0011 is reserved for a
// concurrent change (GH #330), so this lands as 0012.
func TestOwnerBundleTwelveAddsQuarantineState(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 12 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 12 is missing")
	}
	for _, needle := range []string{
		"jobs_state_check",
		"DROP CONSTRAINT jobs_state_check",
		"ADD CONSTRAINT jobs_state_check",
		"'quarantined'",
		"NOT LIKE '%quarantined%'",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 12 missing %q", needle)
		}
	}
	// Ownership-safety: it ALTERs the owner-held jobs table (that is the whole
	// point of the bundle), and it must NOT touch any other owner table.
	if !strings.Contains(bundle.SQL, "ALTER TABLE striatumd.jobs") {
		t.Fatal("bundle 12 must ALTER striatumd.jobs")
	}
}

// TestOwnerBundleThirteenAddsBarrierAssemblyJobType is RFC 0135 P2 (D215/D216): the
// barrier_assembly job_type CHECK widening ships as OWNER BUNDLE 0013 (the
// owner-held jobs CHECK cannot be widened by a runtime migration; D187/#244). It
// mirrors bundle 0012 EXACTLY — an idempotent DROP+re-ADD of jobs_job_type_check
// guarded by a pg_get_constraintdef probe. The older RFC 0132 dissent_quarantine
// 0014 reservation was superseded once GH #372/#379 consumed bundle 0014.
func TestOwnerBundleThirteenAddsBarrierAssemblyJobType(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 13 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 13 is missing")
	}
	for _, needle := range []string{
		"jobs_job_type_check",
		"DROP CONSTRAINT jobs_job_type_check",
		"ADD CONSTRAINT jobs_job_type_check",
		"'barrier_assembly'",
		"NOT LIKE '%barrier_assembly%'",
		"dissent_quarantine",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 13 missing %q", needle)
		}
	}
	// Ownership-safety: it ALTERs the owner-held jobs table (the whole point), and
	// it must NOT touch any other owner table.
	if !strings.Contains(bundle.SQL, "ALTER TABLE striatumd.jobs") {
		t.Fatal("bundle 13 must ALTER striatumd.jobs")
	}
	for _, forbidden := range []string{
		"ALTER TABLE striatumd.runs",
		"ALTER TABLE striatumd.sessions",
		"ALTER TABLE striatumd.events",
	} {
		if strings.Contains(bundle.SQL, forbidden) {
			t.Fatalf("bundle 13 must not contain %q", forbidden)
		}
	}
	// LatestOwnerBundleVersion must include 13.
	if LatestOwnerBundleVersion < 13 {
		t.Fatalf("LatestOwnerBundleVersion = %d; want >= 13 (owner bundle 0013 shipped)", LatestOwnerBundleVersion)
	}
	if ownerBundleLabels[13] == "" {
		t.Fatal("owner bundle 13 has no label in ownerBundleLabels")
	}
}

func TestOwnerBundleFourteenAddsChainLockWaitGauges(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 14 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 14 is missing")
	}
	for _, needle := range []string{
		"ALTER TABLE striatumd.events",
		"ALTER TABLE striatumd.audit_log",
		"ADD COLUMN IF NOT EXISTS lock_wait_us bigint",
		"CREATE OR REPLACE FUNCTION striatumd.append_event_row",
		"CREATE OR REPLACE FUNCTION striatumd.append_audit_row",
		"SECURITY DEFINER",
		"PERFORM striatumd.assert_daemon_authority()",
		"striatumd.audit_v3_row_hash",
		"striatumd.event_v3_row_hash",
		"clock_timestamp()",
		"FROM striatumd.repo_event_chain_heads",
		"FROM striatumd.audit_chain_head",
		"FOR UPDATE",
		"lock_wait_us",
		"REVOKE ALL ON FUNCTION striatumd.append_audit_row",
		"REVOKE ALL ON FUNCTION striatumd.append_event_row",
		"GRANT EXECUTE ON FUNCTION striatumd.append_audit_row",
		"GRANT EXECUTE ON FUNCTION striatumd.append_event_row",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 14 missing %q", needle)
		}
	}
	for _, forbidden := range []string{
		"CREATE INDEX",
		"CREATE UNIQUE INDEX",
	} {
		if strings.Contains(bundle.SQL, forbidden) {
			t.Fatalf("bundle 14 must not add an index, found %q", forbidden)
		}
	}
	if LatestOwnerBundleVersion < 14 {
		t.Fatalf("LatestOwnerBundleVersion = %d; want >= 14 (owner bundle 0014 shipped)", LatestOwnerBundleVersion)
	}
	if ownerBundleLabels[14] == "" {
		t.Fatal("owner bundle 14 has no label in ownerBundleLabels")
	}
}

// TestOwnerBundleSixteenAddsVerifyJobType is RFC 0134 / D227: the 'verify'
// job_type CHECK widening ships as OWNER BUNDLE 0016 (the owner-held jobs CHECK
// cannot be widened by a runtime migration; D215 / D187 / #244). It mirrors
// bundle 0013 — an idempotent DROP+re-ADD of jobs_job_type_check guarded by a
// pg_get_constraintdef probe — and MUST carry barrier_assembly (the prior live
// value, from bundle 0013) alongside the new 'verify' so the DROP+re-ADD does not
// orphan in-flight barrier_assembly jobs.
func TestOwnerBundleSixteenAddsVerifyJobType(t *testing.T) {
	bundles, err := OwnerBundles()
	if err != nil {
		t.Fatalf("OwnerBundles: %v", err)
	}
	var bundle *OwnerBundle
	for index := range bundles {
		if bundles[index].Version == 16 {
			bundle = &bundles[index]
			break
		}
	}
	if bundle == nil {
		t.Fatal("owner bundle 16 is missing")
	}
	for _, needle := range []string{
		"jobs_job_type_check",
		"DROP CONSTRAINT jobs_job_type_check",
		"ADD CONSTRAINT jobs_job_type_check",
		"'verify'",
		"'barrier_assembly'",
		"NOT LIKE '%verify%'",
	} {
		if !strings.Contains(bundle.SQL, needle) {
			t.Fatalf("bundle 16 missing %q", needle)
		}
	}
	// Ownership-safety: it ALTERs the owner-held jobs table (the whole point), and
	// it must NOT touch any other owner table.
	if !strings.Contains(bundle.SQL, "ALTER TABLE striatumd.jobs") {
		t.Fatal("bundle 16 must ALTER striatumd.jobs")
	}
	for _, forbidden := range []string{
		"ALTER TABLE striatumd.runs",
		"ALTER TABLE striatumd.sessions",
		"ALTER TABLE striatumd.events",
	} {
		if strings.Contains(bundle.SQL, forbidden) {
			t.Fatalf("bundle 16 must not contain %q", forbidden)
		}
	}
	if LatestOwnerBundleVersion < 16 {
		t.Fatalf("LatestOwnerBundleVersion = %d; want >= 16 (owner bundle 0016 shipped)", LatestOwnerBundleVersion)
	}
	if ownerBundleLabels[16] == "" {
		t.Fatal("owner bundle 16 has no label in ownerBundleLabels")
	}
}
