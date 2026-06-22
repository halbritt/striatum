package db

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RFC 0142 Layer 1b (D258): the ownership pre-flight lint, promoted from a
// build-time-only test (TestFutureRuntimeMigrationsDoNotCarryOwnerDDL /
// TestFutureRuntimeMigrationsDoNotFKOwnerHeldTables) to a LOAD-TIME refusal. The
// migration loader (ApplyMigrations) runs the SAME static detection over the
// runtime migrations before applying any of them, and refuses to apply anything
// if a runtime migration ALTERs/DROPs or FK-references an owner-held relation.
//
// Why both layers read THIS one source ("one source, no drift", RFC 0142):
// detection lives here (non-test) so the loader guard and the build-time guards
// in migrations_test.go cannot disagree about which operations are forbidden.
// The owner-held determination comes from db.RuntimeOwnedTablesAlterable() (the
// owner-bundle-derived transfer set in owner_runtime_ownership.go), the same
// authoritative ownership split the live two-role pgtest fixture reads.
//
// This pre-filter is deliberately CONSERVATIVE and is NOT sound: a migration can
// construct relation names dynamically (DO blocks, EXECUTE format(),
// search_path-relative names) and evade a static parser (RFC 0142's #1
// load-bearing risk). The real oracle is the P0 two-role pgtest fixture / live
// pg_class.relowner check; this guard only denies the clear static cases (a
// literal `ALTER striatumd.<owner table>` or `REFERENCES striatumd.<owner
// table>`), routing the author to an owner bundle before a statement runs.

// futureRuntimeMigrationOwnerDDLFloor scopes both the build-time guards and the
// load-time pre-flight: only runtime migrations at or above this version are
// checked. Pre-split migrations were applied as the owner at bootstrap, so their
// grandfathered owner-table DDL / FKs are out of scope for the two-role guard.
const futureRuntimeMigrationOwnerDDLFloor = 27

var runtimeMigrationOwnerDDLPattern = regexp.MustCompile(`(?is)\b(?:ALTER|DROP)\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(striatumd\.[a-z_][a-z0-9_]*)`)

// runtimeMigrationOwnerFKPattern matches an inbound foreign-key reference to a
// striatumd.* table, in either the inline-column form
// (`... REFERENCES striatumd.<table>`) or the table-constraint form
// (`FOREIGN KEY (...) REFERENCES striatumd.<table>`). Both spellings contain the
// `REFERENCES striatumd.<table>` token, so one pattern captures the FK target.
var runtimeMigrationOwnerFKPattern = regexp.MustCompile(`(?is)\bREFERENCES\s+(striatumd\.[a-z_][a-z0-9_]*)`)

// runtimeMigrationCreateTablePattern captures every striatumd.* table a single
// migration CREATEs; a brand-new runtime table is striatumd_rw-owned at create
// time on a two-role deploy, so a foreign key into it (including a
// self-reference) is legitimately runtime-applyable.
var runtimeMigrationCreateTablePattern = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(striatumd\.[a-z_][a-z0-9_]*)`)

// sqlLineCommentPattern strips `-- …` to end-of-line so comment prose (every
// above-floor runtime migration documents "NO foreign key into the owner-held …"
// in a comment) is never mistaken for real FK DDL. The runtime migration SQL set
// uses only `--` line comments — there are no `/* … */` block comments — so this
// is sufficient.
var sqlLineCommentPattern = regexp.MustCompile(`(?m)--.*$`)

// runtimeMigrationOwnerDDLViolations returns the distinct `ALTER`/`DROP TABLE`
// statements in a runtime migration whose target striatumd.* table is NOT
// runtime-owned (i.e. owner-held) — the operations striatumd_rw cannot perform
// on a two-role deploy (42501 at boot). runtimeOwned is the owner-bundle-derived
// transfer set (RuntimeOwnedTablesAlterable); a table an owner bundle has
// transferred to striatumd_rw is legitimately ALTERable (GH #442).
func runtimeMigrationOwnerDDLViolations(migration Migration, runtimeOwned map[string]bool) []string {
	matches := runtimeMigrationOwnerDDLPattern.FindAllStringSubmatch(migration.SQL, -1)
	violations := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		cleaned := strings.Join(strings.Fields(match[0]), " ")
		table := strings.ToLower(match[1])
		if runtimeOwned[table] {
			// A table an owner bundle has transferred to striatumd_rw ownership;
			// the runtime role may legitimately ALTER it (ownership-aware, GH #442).
			continue
		}
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		violations = append(violations, cleaned)
	}
	sort.Strings(violations)
	return violations
}

// runtimeMigrationOwnerFKViolations is the GENERIC complement to the owner-DDL
// guard (issue #508): for a runtime migration it returns every inbound foreign
// key whose target table is NOT runtime-owned — i.e. an FK into an owner-held
// table. On a two-role production deploy (RFC 0110 / D215) runtime migrations are
// applied as striatumd_rw, which has no REFERENCES privilege on owner-held
// tables, so such an FK fails with `permission denied for table <owner table>`
// (SQLSTATE 42501) at migrate time and crash-loops the daemon (the migration 0041
// / D242 incident, fixed for that instance in D248 / #507; pgtest's single-role
// DB masked it).
//
// The owner-held determination is DERIVED from the same authoritative ownership
// split the owner-DDL guard uses: a table is runtime-owned only when an owner
// bundle has transferred its ownership to striatumd_rw (runtimeOwned), or when
// THIS migration itself CREATEs it (a fresh runtime-role-owned table). Anything
// else a runtime migration references is owner-held — so this guard needs no
// hand-maintained per-migration `forbidden` list (the hole 0041 fell through),
// and the whole class is impossible to reintroduce above the floor.
func runtimeMigrationOwnerFKViolations(migration Migration, runtimeOwned map[string]bool) []string {
	body := sqlLineCommentPattern.ReplaceAllString(migration.SQL, "")

	createdHere := map[string]bool{}
	for _, match := range runtimeMigrationCreateTablePattern.FindAllStringSubmatch(body, -1) {
		createdHere[strings.ToLower(match[1])] = true
	}

	violations := make([]string, 0)
	seen := map[string]bool{}
	for _, match := range runtimeMigrationOwnerFKPattern.FindAllStringSubmatch(body, -1) {
		target := strings.ToLower(match[1])
		if runtimeOwned[target] || createdHere[target] {
			// A runtime-owned table (owner-bundle transferred) or one created by
			// this same migration: striatumd_rw owns it, so an inbound FK applies
			// cleanly under the runtime role.
			continue
		}
		if seen[target] {
			continue
		}
		seen[target] = true
		violations = append(violations, "REFERENCES "+target)
	}
	sort.Strings(violations)
	return violations
}

// ErrRuntimeMigrationOwnerDDL is the typed refusal the migration loader returns
// when an above-floor runtime migration carries owner-table DDL or an inbound
// owner-table foreign key. Callers (ConnectAndMigrate) propagate it; the loader
// guarantees it has applied NOTHING when it refuses, so the database is left
// untouched and the daemon does not crash-loop a half-applied deploy.
var ErrRuntimeMigrationOwnerDDL = errors.New("runtime migration carries owner-held-table DDL or foreign key")

// preflightRuntimeMigrationOwnership runs the static ownership lint over the
// above-floor runtime migrations and returns ErrRuntimeMigrationOwnerDDL (with a
// detailed message naming the migration and the offending operations) if any
// migration ALTERs/DROPs or FK-references an owner-held relation. It is the
// load-time form of the build-time guards: the loader calls it BEFORE applying
// any migration so a refusal applies nothing.
//
// The owner-held set is db.RuntimeOwnedTablesAlterable() — the same
// owner-bundle-derived transfer set the build-time guards and the live two-role
// fixture read ("one source, no drift").
func preflightRuntimeMigrationOwnership(migrations []Migration) error {
	runtimeOwned, err := RuntimeOwnedTablesAlterable()
	if err != nil {
		return fmt.Errorf("derive runtime-owned tables from owner bundles for ownership pre-flight: %w", err)
	}
	var refusals []string
	for _, migration := range migrations {
		if migration.Version < futureRuntimeMigrationOwnerDDLFloor {
			continue
		}
		if ddl := runtimeMigrationOwnerDDLViolations(migration, runtimeOwned); len(ddl) > 0 {
			refusals = append(refusals, fmt.Sprintf("migration %d ALTER/DROPs an owner-held table: %v", migration.Version, ddl))
		}
		if fks := runtimeMigrationOwnerFKViolations(migration, runtimeOwned); len(fks) > 0 {
			refusals = append(refusals, fmt.Sprintf("migration %d foreign-keys into an owner-held table: %v", migration.Version, fks))
		}
	}
	if len(refusals) > 0 {
		return fmt.Errorf(
			"%w (striatumd_rw lacks the privilege to apply these; a two-role deploy would crash-loop with SQLSTATE 42501 — move the DDL to an owner bundle or enforce the referential integrity in Go): %s",
			ErrRuntimeMigrationOwnerDDL,
			strings.Join(refusals, "; "),
		)
	}
	return nil
}
