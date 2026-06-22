package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPreflightRefusesCraftedOwnerTableRuntimeMigration is the RFC 0142 Layer 1b
// (D258) load-time refusal proof: the pre-flight the migration loader runs
// REFUSES a crafted above-floor runtime migration that ALTERs an owner-held
// table, returning the typed ErrRuntimeMigrationOwnerDDL — exactly the #442
// crash-loop the build-time guard catches, now caught at load time before any
// statement runs.
func TestPreflightRefusesCraftedOwnerTableRuntimeMigration(t *testing.T) {
	bad := []Migration{{
		Version: futureRuntimeMigrationOwnerDDLFloor,
		Label:   "crafted owner-table ALTER",
		SQL:     "ALTER TABLE striatumd.runs ADD COLUMN IF NOT EXISTS unsafe text;",
	}}

	err := preflightRuntimeMigrationOwnership(bad)
	if err == nil {
		t.Fatal("pre-flight must refuse a runtime migration that ALTERs an owner-held table")
	}
	if !errors.Is(err, ErrRuntimeMigrationOwnerDDL) {
		t.Fatalf("pre-flight refusal must wrap ErrRuntimeMigrationOwnerDDL, got %v", err)
	}
	if !strings.Contains(err.Error(), "striatumd.runs") {
		t.Fatalf("refusal message must name the offending owner table, got %v", err)
	}
}

// TestPreflightRefusesCraftedOwnerTableFK proves the load-time refusal also fires
// on the FK form (the migration 0041 / D248 incident): a crafted runtime
// migration whose CREATE TABLE foreign-keys into the owner-held repositories
// table is refused with the typed error.
func TestPreflightRefusesCraftedOwnerTableFK(t *testing.T) {
	bad := []Migration{{
		Version: futureRuntimeMigrationOwnerDDLFloor,
		Label:   "crafted owner-table FK",
		SQL: `
			CREATE TABLE IF NOT EXISTS striatumd.bad_ledger (
			  id text PRIMARY KEY,
			  repository_id text NOT NULL REFERENCES striatumd.repositories(repository_id)
			);
		`,
	}}

	err := preflightRuntimeMigrationOwnership(bad)
	if err == nil {
		t.Fatal("pre-flight must refuse a runtime migration that FK-references an owner-held table")
	}
	if !errors.Is(err, ErrRuntimeMigrationOwnerDDL) {
		t.Fatalf("pre-flight refusal must wrap ErrRuntimeMigrationOwnerDDL, got %v", err)
	}
	if !strings.Contains(err.Error(), "REFERENCES striatumd.repositories") {
		t.Fatalf("refusal message must name the offending FK target, got %v", err)
	}
}

// TestPreflightAppliesCleanRuntimeMigrationSet proves the pre-flight is NOT a
// blanket denial: a clean runtime migration that creates a new runtime-owned
// table and FK-references only itself / a runtime-owned table passes, and an
// above-floor migration that ALTERs a table an owner bundle has TRANSFERRED to
// striatumd_rw also passes (the ownership-aware allowance, GH #442).
func TestPreflightAppliesCleanRuntimeMigrationSet(t *testing.T) {
	runtimeOwned, err := RuntimeOwnedTablesAlterable()
	if err != nil {
		t.Fatalf("derive runtime-owned tables: %v", err)
	}
	if !runtimeOwned["striatumd.job_recovery_state"] {
		t.Fatal("precondition: owner bundle 0018 must transfer job_recovery_state to striatumd_rw")
	}

	clean := []Migration{
		{
			Version: futureRuntimeMigrationOwnerDDLFloor,
			Label:   "clean new runtime table",
			SQL: `
				-- NO foreign key into any owner-held table; this comment must be ignored.
				CREATE TABLE IF NOT EXISTS striatumd.clean_child (
				  id text PRIMARY KEY,
				  parent_id text REFERENCES striatumd.clean_child(id),
				  recovery_id text REFERENCES striatumd.job_recovery_state(id)
				);
			`,
		},
		{
			Version: futureRuntimeMigrationOwnerDDLFloor + 1,
			Label:   "ownership-aware ALTER of a transferred table",
			SQL:     "ALTER TABLE striatumd.job_recovery_state ADD COLUMN IF NOT EXISTS extra text;",
		},
	}

	if err := preflightRuntimeMigrationOwnership(clean); err != nil {
		t.Fatalf("pre-flight must pass a clean runtime migration set, got %v", err)
	}
}

// TestPreflightSkipsBelowFloorMigrations proves the load-time refusal keeps the
// SAME floor scoping the build-time guard uses: a pre-split (below-floor)
// migration that touches an owner table is NOT refused (those were applied as the
// owner at bootstrap), so the pre-flight only ever reds NEW above-floor offenders.
func TestPreflightSkipsBelowFloorMigrations(t *testing.T) {
	belowFloor := []Migration{{
		Version: futureRuntimeMigrationOwnerDDLFloor - 1,
		Label:   "grandfathered below-floor owner DDL",
		SQL:     "ALTER TABLE striatumd.runs ADD COLUMN IF NOT EXISTS grandfathered text;",
	}}
	if err := preflightRuntimeMigrationOwnership(belowFloor); err != nil {
		t.Fatalf("pre-flight must NOT refuse a below-floor migration (out of scope for the two-role guard), got %v", err)
	}
}

// TestLiveEmbeddedMigrationsPassPreflight pins the live tree green: the real
// embedded runtime migration set passes the load-time pre-flight, so a clean
// daemon boot is never refused by this guard (the guard reds only on a NEW
// owner-table-touching runtime migration, which the build-time guards would also
// red).
func TestLiveEmbeddedMigrationsPassPreflight(t *testing.T) {
	migrations, err := Migrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := preflightRuntimeMigrationOwnership(migrations); err != nil {
		t.Fatalf("the live embedded migration set must pass the load-time pre-flight, got %v", err)
	}
}

// TestApplyMigrationsStillAppliesCleanSet is the end-to-end loader proof against
// the fake runner: with the live (clean) embedded set, ApplyMigrations runs the
// pre-flight and then applies through to LatestDaemonDBVersion, recording the
// substrate version — i.e. the guard does not break the normal apply path.
// (The refusal direction cannot be exercised through ApplyMigrations without a
// crafted embedded FS; the pre-flight refusal is proven directly above, and the
// loader calls it BEFORE the apply loop so a refusal applies nothing.)
func TestApplyMigrationsStillAppliesCleanSet(t *testing.T) {
	runner := &fakeRunner{scalars: map[string]string{}}
	version, err := ApplyMigrations(context.Background(), runner, "test")
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if version != LatestDaemonDBVersion {
		t.Fatalf("version = %d, want %d", version, LatestDaemonDBVersion)
	}
	joined := strings.Join(runner.execs, "\n")
	if !strings.Contains(joined, "substrate_version") {
		t.Fatalf("substrate version was not recorded after the clean-set apply")
	}
}
