package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/jackc/pgx/v5/pgconn"
)

// RFC 0142 P0 — the two-role pgtest fixture's executable oracle for the #442 /
// D248 two-role 42501 trap. These tests run as a privilege-constrained,
// escape-proof striatumd_rw login role (C1) against a database bootstrapped to
// production's owner/runtime ownership split (C2), and assert SQLSTATE 42501
// fires on an owner-held-table touch but NOT on a legal runtime operation — the
// discrimination the single-role pgtest.Pools path cannot reproduce. They need a
// real two-role cluster (STRIATUM_PG_TEST_URL); the no-network verifier sandbox
// skips them via pgtest.TwoRole.

// TestRuntimeMigrationOwnerTableTouchIsDeniedTwoRole is the C1 red regression: as
// the dedicated SUT login role, an owner-held-table ALTER (the #442 shape) and an
// inbound FK into an owner-held table (the D248 / #507 shape) both fail SQLSTATE
// 42501 — and a leading `RESET ROLE` does NOT hand back owner power, because the
// SUT's login user is itself a constrained non-owner role (C1's escape-proof
// gate), not the owner with `SET ROLE striatumd_rw` layered on top.
func TestRuntimeMigrationOwnerTableTouchIsDeniedTwoRole(t *testing.T) {
	fx := pgtest.TwoRole(t)
	ctx := context.Background()

	// #442: a runtime ALTER of the owner-held events table → "must be owner of
	// table events" (42501). The RESET ROLE proves there is no path back to the
	// owner/DSN login: it falls back to the constrained SUT login, which still
	// cannot own events.
	err := fx.SUTPool.Runner.Exec(ctx,
		"RESET ROLE; ALTER TABLE striatumd.events ADD COLUMN p0_probe integer")
	assertSQLState42501(t, err, "must be owner of table events",
		"RESET ROLE + owner-held ALTER (events) as the SUT role")

	// D248 / #507: an inbound FK into the owner-held repositories table →
	// "permission denied for table repositories" (42501). The runtime role holds
	// DML on repositories but no REFERENCES privilege, and role membership conveys
	// neither ownership nor REFERENCES. The message guard distinguishes this real
	// FK denial from an unrelated 42501 (e.g. a missing CREATE-on-schema setup).
	err = fx.SUTPool.Runner.Exec(ctx, `
		CREATE TABLE striatumd.p0_probe_child (
		  id text PRIMARY KEY,
		  repository_id text REFERENCES striatumd.repositories(repository_id)
		)`)
	assertSQLState42501(t, err, "permission denied for table repositories",
		"owner-held FK (repositories) as the SUT role")
}

// TestRuntimeMigrationRuntimeOwnedTableAlterSucceedsTwoRole is the green control:
// the SAME ALTER verb denied on the owner-held events table SUCCEEDS on every
// striatumd_rw-owned table — proving the fixture discriminates by OWNERSHIP, not
// by blanket-denying DDL (no false reds). It covers both an owner-bundle-
// transferred table and the recent above-floor tables the C2 fidelity makes
// striatumd_rw-owned.
func TestRuntimeMigrationRuntimeOwnedTableAlterSucceedsTwoRole(t *testing.T) {
	fx := pgtest.TwoRole(t)
	ctx := context.Background()

	for _, table := range []string{
		"job_recovery_state",          // transferred by owner bundle 0018
		"verifier_attestations",       // CREATEd above the floor (0042) by striatumd_rw
		"event_chain_segments",        // 0041 (the D242 incident table)
		"supervisor_buffered_packets", // 0038
	} {
		stmt := "ALTER TABLE striatumd." + table + " ADD COLUMN p0_probe integer"
		if err := fx.SUTPool.Runner.Exec(ctx, stmt); err != nil {
			t.Fatalf("legal runtime ALTER on the striatumd_rw-owned table %s must succeed under the two-role fixture (a false red breaks the no-false-reds half of the claim): %v", table, err)
		}
	}

	// A legal runtime DML and a brand-new runtime table also succeed (a fresh
	// runtime table is striatumd_rw-owned at create time). A SELECT on a runtime
	// coordination table proves the read DML grant without coupling to that table's
	// NOT NULL columns (which would fail for the wrong reason).
	if err := fx.SUTPool.Runner.Exec(ctx,
		"SELECT count(*) FROM striatumd.leases"); err != nil {
		t.Fatalf("a legal runtime SELECT on a runtime DML table must succeed under the SUT role: %v", err)
	}
	if err := fx.SUTPool.Runner.Exec(ctx,
		"CREATE TABLE striatumd.p0_new (id text PRIMARY KEY)"); err != nil {
		t.Fatalf("a brand-new runtime CREATE TABLE must succeed under the SUT role: %v", err)
	}
}

// TestTwoRoleBootstrapOwnershipFidelity is the C2 differential / one-source
// check: the live pg_class.relowner after the real owner bundles apply must AGREE
// with the static-guard-derived runtime-owned set (db.RuntimeOwnedTablesAlterable,
// the same owner-bundle SQL the parse-time migration guards read), and the recent
// runtime-created tables must be striatumd_rw-owned (the bug the Phase-A-as-owner
// bootstrap had). If the two derivations disagree, the static guard is asserting
// against the wrong ground truth.
func TestTwoRoleBootstrapOwnershipFidelity(t *testing.T) {
	fx := pgtest.TwoRole(t)
	ctx := context.Background()

	runtimeOwned, err := db.RuntimeOwnedTablesAlterable()
	if err != nil {
		t.Fatalf("derive runtime-owned tables: %v", err)
	}
	if len(runtimeOwned) == 0 {
		t.Fatal("precondition: the owner bundles must transfer at least one table to striatumd_rw (bundle 0018); an empty set means the derivation regressed")
	}
	// Every table the static guard treats as runtime-owned must be live
	// striatumd_rw-owned — otherwise the parse-time guard and the live oracle have
	// drifted apart ("one source, no drift" violated).
	for qualified := range runtimeOwned {
		if owner := liveOwner(t, ctx, fx.OwnerPool.Runner, qualified); owner != "striatumd_rw" {
			t.Fatalf("one-source drift: the static guard treats %s as runtime-owned, but live relowner = %q (the bundle transfer did not land, so the static guard asserts against the wrong ground truth)", qualified, owner)
		}
	}

	// The recent runtime-created tables (never transferred by a bundle) must be
	// striatumd_rw-owned because the fixture applied the above-floor migrations AS
	// striatumd_rw — the C2 fidelity the Phase-A-as-owner bootstrap got wrong.
	for _, qualified := range []string{
		"striatumd.supervisor_buffered_packets",
		"striatumd.event_chain_segments",
		"striatumd.verifier_attestations",
	} {
		if owner := liveOwner(t, ctx, fx.OwnerPool.Runner, qualified); owner != "striatumd_rw" {
			t.Fatalf("C2: recent runtime table %s owner = %q, want striatumd_rw (apply above-floor migrations as the runtime role, not the owner, or a legal runtime ALTER false-reds)", qualified, owner)
		}
	}

	// The owner-held probe table is genuinely owner-held — not striatumd_rw — so
	// the red 42501 oracle is meaningful and not a blanket failure.
	if owner := liveOwner(t, ctx, fx.OwnerPool.Runner, "striatumd.events"); owner == "striatumd_rw" {
		t.Fatal("C2: striatumd.events is striatumd_rw-owned; it must stay owner-held or the red oracle is void")
	}
}

// TestTwoRoleCatchesDynamicOwnerTableTouch demonstrates the executable-oracle
// value over the parse-time guard (RFC 0142 §3.7): a DYNAMIC owner-table ALTER the
// static regex scanner cannot resolve still fails 42501 under the live fixture —
// "static SQL resolution is unsound; the fixture is the real oracle", proven with
// a test, not an assertion.
func TestTwoRoleCatchesDynamicOwnerTableTouch(t *testing.T) {
	fx := pgtest.TwoRole(t)
	ctx := context.Background()

	err := fx.SUTPool.Runner.Exec(ctx, `
		DO $$ BEGIN
		  EXECUTE 'ALTER TABLE striatumd.events ADD COLUMN p0_dyn integer';
		END $$;`)
	// The inner ALTER re-raises through PL/pgSQL with its original SQLSTATE; only
	// the code is asserted (the message is wrapped by the DO block context).
	assertSQLState42501(t, err, "",
		"dynamic (DO/EXECUTE) owner-held ALTER as the SUT role")
}

// assertSQLState42501 asserts err is a PostgreSQL 42501 insufficient_privilege
// failure (optionally carrying msgContains), so a setup breakage or an unrelated
// error can never masquerade as the real two-role denial.
func assertSQLState42501(t *testing.T, err error, msgContains, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s unexpectedly SUCCEEDED; the two-role fixture is not exercising the privilege boundary it claims to (a false green)", what)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("%s failed with a non-PostgreSQL error: %v", what, err)
	}
	if pgErr.Code != "42501" {
		t.Fatalf("%s failed with SQLSTATE %s (%s); want 42501 insufficient_privilege (the #442 / D248 crash-loop signature)", what, pgErr.Code, pgErr.Message)
	}
	if msgContains != "" && !strings.Contains(pgErr.Message, msgContains) {
		t.Fatalf("%s failed 42501 but message %q does not contain %q (a setup breakage could otherwise masquerade as the real denial)", what, pgErr.Message, msgContains)
	}
}

func liveOwner(t *testing.T, ctx context.Context, runner db.Runner, qualified string) string {
	t.Helper()
	owner, err := runner.QueryScalar(ctx,
		"SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid = $1::regclass", qualified)
	if err != nil {
		t.Fatalf("read live owner of %s: %v", qualified, err)
	}
	return owner
}
