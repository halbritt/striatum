package db

import (
	"regexp"
	"strings"
)

// The owner bundles are the single source of truth for which striatumd.* tables
// an above-floor runtime migration may legitimately ALTER: a table is
// runtime-owned (alterable by striatumd_rw) only once an owner bundle has
// TRANSFERRED its ownership to striatumd_rw (RFC 0110 / RFC 0142, GH #442/#441).
// Both the static migration guards (TestFutureRuntimeMigrationsDoNotCarryOwnerDDL
// and its siblings in migrations_test.go) and the live two-role pgtest fixture
// (pgtest.TwoRole, RFC 0142 P0) derive "owner-held" from THIS one derivation, so
// the parse-time verdict and the live pg_class.relowner oracle cannot drift
// ("one source, no drift"). RFC 0142 holder R4 calls for exposing the
// transferred-set derivation as a shared helper precisely so the live oracle and
// the static guard read the same source; this file is that helper.

// ownerBundleLiteralTransferPattern matches a literal
// `ALTER TABLE [ONLY] striatumd.<name> OWNER TO striatumd_rw` ownership transfer
// in an owner bundle (the direct, non-loop form). The captured table is then
// alterable by a runtime migration.
var ownerBundleLiteralTransferPattern = regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(?:ONLY\s+)?striatumd\.([a-z_][a-z0-9_]*)\s+OWNER\s+TO\s+striatumd_rw`)

// ownerBundleCohortArrayPattern / ownerBundleFormatTransferPattern /
// cohortTableNamePattern match the cohort-loop transfer form owner bundle 0018
// uses: a `format('ALTER TABLE striatumd.%I OWNER TO striatumd_rw', …)` over a
// `v_cohort … ARRAY[ … ]` literal. When a bundle carries BOTH the cohort array
// and the `format(... OWNER TO striatumd_rw)` transfer over it, every
// single-quoted name inside that ARRAY[…] block is a table the bundle transfers
// to runtime ownership.
var ownerBundleCohortArrayPattern = regexp.MustCompile(`(?is)ARRAY\s*\[(.*?)\]`)
var ownerBundleFormatTransferPattern = regexp.MustCompile(`(?is)format\([^)]*ALTER\s+TABLE\s+striatumd\.%I\s+OWNER\s+TO\s+striatumd_rw`)
var cohortTableNamePattern = regexp.MustCompile(`'([a-z_][a-z0-9_]*)'`)

// RuntimeOwnedTablesAlterable returns the set of striatumd.* tables (keyed
// "striatumd.<name>") whose ownership the embedded owner bundles transfer to
// striatumd_rw, and which a runtime migration may therefore legitimately ALTER.
//
// It is DERIVED from the owner bundle SQL, never a hardcoded list: if owner
// bundle 0018 is ever dropped the set goes empty and the above-floor ALTERs of
// job_recovery_state / barrier_staged_contributions immediately fail their guards
// — exactly the D187/D215 trap, no escape.
//
// This is the shared derivation the static migration guards and the live
// two-role pgtest fixture both read, so the static parse-time verdict and the
// live pg_class.relowner oracle cannot disagree (RFC 0142 "one source, no
// drift"). It is pure (reads only the embedded owner bundle files) and changes no
// daemon behavior.
func RuntimeOwnedTablesAlterable() (map[string]bool, error) {
	bundles, err := OwnerBundles()
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, bundle := range bundles {
		// Direct literal transfers: ALTER TABLE striatumd.<name> OWNER TO striatumd_rw.
		for _, m := range ownerBundleLiteralTransferPattern.FindAllStringSubmatch(bundle.SQL, -1) {
			allowed["striatumd."+strings.ToLower(m[1])] = true
		}
		// Cohort-loop transfers: a format('ALTER TABLE striatumd.%I OWNER TO
		// striatumd_rw', …) over a v_cohort ARRAY[…] literal. Only extract the
		// cohort-array names, and only when the transfer-over-the-cohort is present,
		// so unrelated quoted strings (role names, stamp capabilities) cannot widen
		// the allowlist.
		if ownerBundleFormatTransferPattern.MatchString(bundle.SQL) {
			for _, arr := range ownerBundleCohortArrayPattern.FindAllStringSubmatch(bundle.SQL, -1) {
				for _, m := range cohortTableNamePattern.FindAllStringSubmatch(arr[1], -1) {
					allowed["striatumd."+strings.ToLower(m[1])] = true
				}
			}
		}
	}
	return allowed, nil
}
