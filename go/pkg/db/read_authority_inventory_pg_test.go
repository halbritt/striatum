package db_test

import (
	"context"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestReadAuthorityInventoryComplete is the #164 guard: every daemon-owned
// table has an explicit read classification, so the current broad-runtime-SELECT
// posture cannot grow silently while the least-privilege split is still open.
func TestReadAuthorityInventoryComplete(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	rs, err := pool.RawPool.Query(ctx,
		`SELECT table_name FROM information_schema.tables
		  WHERE table_schema = 'striatumd' AND table_type = 'BASE TABLE'
		  ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rs.Close()

	var unclassified []string
	count := 0
	for rs.Next() {
		var name string
		if err := rs.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		count++
		if _, ok := db.ClassifyReadTable(name); !ok {
			unclassified = append(unclassified, name)
		}
	}
	if err := rs.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	if count == 0 {
		t.Fatal("no striatumd tables found; the read inventory guard would be vacuous")
	}
	if len(unclassified) > 0 {
		t.Fatalf("striatumd tables missing a read-authority inventory row (#164): %v\n"+
			"add each to readAuthorityInventory in read_authority_inventory.go", unclassified)
	}
}

// TestReadDeniedTablesHaveNoRuntimeSelect keeps the authority registry and
// other owner-only read surfaces denied to the runtime role while #164 works on
// reducing the still-broad selectable surface.
func TestReadDeniedTablesHaveNoRuntimeSelect(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	for table, class := range db.ReadAuthorityInventory() {
		if class != db.ReadClassRuntimeDenied {
			continue
		}
		if scalar(t, ctx, pool.Runner,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'SELECT')::text", table) != "false" {
			t.Fatalf("striatumd_rw can SELECT %s; read inventory class is %s", table, class)
		}
	}
}

// TestRuntimeParityTablesHaveRuntimeSelect pins the owner-maintained tables the
// daemon intentionally reads through the runtime role during startup.
func TestRuntimeParityTablesHaveRuntimeSelect(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	found := 0
	for table, class := range db.ReadAuthorityInventory() {
		if class != db.ReadClassRuntimeParity {
			continue
		}
		found++
		if scalar(t, ctx, pool.Runner,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'SELECT')::text", table) != "true" {
			t.Fatalf("striatumd_rw cannot SELECT %s; read inventory class is %s", table, class)
		}
		for _, privilege := range []string{"INSERT", "UPDATE", "DELETE"} {
			if scalar(t, ctx, pool.Runner,
				"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, $2)::text", table, privilege) != "false" {
				t.Fatalf("striatumd_rw can %s %s; runtime parity tables must stay write-owner-only", privilege, table)
			}
		}
	}
	if found == 0 {
		t.Fatal("no runtime_parity_select tables in the inventory; the startup parity guard would be vacuous")
	}
}

// TestClientsReadClassIsColumnScoped formalizes the first P1 least-privilege
// campaign slice: clients is no longer counted as broad runtime-sensitive
// SELECT, but its token secret columns remain explicitly denied.
func TestClientsReadClassIsColumnScoped(t *testing.T) {
	class, ok := db.ClassifyReadTable("clients")
	if !ok || class != db.ReadClassRuntimeColumnScoped {
		t.Fatalf("clients read class = (%q, %v), want (%q, true)",
			class, ok, db.ReadClassRuntimeColumnScoped)
	}
	if containsString("clients", db.RuntimeSensitiveReadTables()) {
		t.Fatal("clients is still listed as a runtime_sensitive_select table")
	}
	if !containsString("clients", db.RuntimeColumnScopedReadTables()) {
		t.Fatal("clients is missing from runtime_column_scoped_select tables")
	}
	denied := db.RuntimeDeniedReadColumns()["clients"]
	if !containsString("token_hash", denied) || !containsString("token_salt", denied) {
		t.Fatalf("clients denied columns = %v, want token_hash/token_salt", denied)
	}
}

// TestReadDeniedColumnsHaveNoRuntimeSelect pins the RFC 0113 R1 column-level
// reduction: client token hashes/salts are no longer directly selectable by the
// runtime role after owner bundle 0005, even though non-secret client metadata
// remains readable while the broader #164 split continues.
func TestReadDeniedColumnsHaveNoRuntimeSelect(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	for table, columns := range db.RuntimeDeniedReadColumns() {
		for _, column := range columns {
			if scalar(t, ctx, pool.Runner,
				"SELECT has_column_privilege('striatumd_rw', 'striatumd.'||$1, $2, 'SELECT')::text", table, column) != "false" {
				t.Fatalf("striatumd_rw can SELECT %s.%s; read inventory marks it denied", table, column)
			}
		}
	}
	if scalar(t, ctx, pool.Runner,
		"SELECT has_column_privilege('striatumd_rw', 'striatumd.clients', 'client_id', 'SELECT')::text") != "true" {
		t.Fatal("striatumd_rw lost non-secret clients.client_id SELECT while only token secret columns should be denied")
	}
	// Positive control for the RFC 0114 principal_clients column gate: the
	// non-gated columns stay directly readable because admin/tokens.go's live
	// `UPDATE ... WHERE client_id = $1 AND unlinked_at IS NULL` reads them.
	for _, column := range []string{"client_id", "linked_at", "unlinked_at"} {
		if scalar(t, ctx, pool.Runner,
			"SELECT has_column_privilege('striatumd_rw', 'striatumd.principal_clients', $1, 'SELECT')::text", column) != "true" {
			t.Fatalf("striatumd_rw lost principal_clients.%s SELECT; only principal_id should be denied (RFC 0114)", column)
		}
	}
}

// TestProjectionReadTablesHaveNoRuntimeSelect pins the RFC 0114 table-level
// closures: every runtime_projection_read table is readable by the daemon only
// through its daemon-authorized projections — the runtime role holds no table
// SELECT after owner bundle 0006.
func TestProjectionReadTablesHaveNoRuntimeSelect(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	found := 0
	for table, class := range db.ReadAuthorityInventory() {
		if class != db.ReadClassRuntimeProjection {
			continue
		}
		found++
		if scalar(t, ctx, pool.Runner,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'SELECT')::text", table) != "false" {
			t.Fatalf("striatumd_rw can SELECT %s; read inventory class is %s", table, class)
		}
	}
	if found == 0 {
		t.Fatal("no runtime_projection_read tables in the inventory; the projection-read guard would be vacuous")
	}
}

// TestClientSessionsRetainRuntimeDML pins that RFC 0114 narrows READS only:
// the runtime role keeps INSERT/UPDATE/DELETE on all three gated identity
// tables (write authority is owned by RFC 0110, untouched here).
func TestClientSessionsRetainRuntimeDML(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	for _, table := range []string{"client_sessions", "principals", "principal_clients"} {
		for _, privilege := range []string{"INSERT", "UPDATE", "DELETE"} {
			if scalar(t, ctx, pool.Runner,
				"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, $2)::text", table, privilege) != "true" {
				t.Fatalf("striatumd_rw lost %s on %s; owner bundle 0006 must narrow reads only", privilege, table)
			}
		}
	}
}

func containsString(needle string, haystack []string) bool {
	for _, item := range haystack {
		if item == needle {
			return true
		}
	}
	return false
}

// TestOwnerTransferClosesSelfRegrant is the executable Option B refutation
// from RFC 0114: a REVOKE SELECT against a runtime-owned table is NOT a
// security boundary, because any member of the owning role can re-grant
// SELECT in one statement — and after ALTER TABLE ... OWNER TO the
// self-re-grant fails. This is the mechanism proof pgtest would otherwise
// mask: the pgtest pool user — not striatumd_rw — owns the real tables there,
// making the bundle's ownership transfer an idempotent no-op in CI.
func TestOwnerTransferClosesSelfRegrant(t *testing.T) {
	ownerPool, runtimePool := pgtest.Pools(t)
	ctx := context.Background()

	if err := ownerPool.Runner.Exec(ctx,
		"CREATE TABLE striatumd.owner_transfer_probe (v text)"); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	// ALTER TABLE ... OWNER TO requires the NEW owner to hold CREATE on the
	// table's schema unless the executor is superuser. The throwaway-cluster
	// lanes ran as the initdb superuser and never hit this; on a shared
	// cluster the pgtest pool user is a plain role, so grant it explicitly —
	// probe scaffolding only, revoked again after the back-transfer.
	if err := ownerPool.Runner.Exec(ctx,
		"GRANT CREATE ON SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("grant schema CREATE for the probe handoff: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx,
		"ALTER TABLE striatumd.owner_transfer_probe OWNER TO striatumd_rw"); err != nil {
		t.Fatalf("hand probe table to the runtime role: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx,
		"REVOKE SELECT ON striatumd.owner_transfer_probe FROM striatumd_rw"); err != nil {
		t.Fatalf("revoke probe SELECT: %v", err)
	}
	if scalar(t, ctx, ownerPool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_transfer_probe', 'SELECT')::text") != "false" {
		t.Fatal("revoke on the runtime-owned probe table did not flip has_table_privilege; setup is wrong")
	}

	// Option B refutation: while the runtime role owns the table, a runtime
	// credential (any member of the owning role) re-grants itself SELECT.
	if err := runtimePool.Runner.Exec(ctx,
		"GRANT SELECT ON striatumd.owner_transfer_probe TO striatumd_rw"); err != nil {
		t.Fatalf("self-re-grant on a runtime-owned table should succeed (the refutation): %v", err)
	}
	if scalar(t, ctx, ownerPool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_transfer_probe', 'SELECT')::text") != "true" {
		t.Fatal("self-re-grant did not reopen SELECT; the Option B refutation is not exercising ownership")
	}

	// Option A: transfer ownership, re-close, and prove the same re-grant now
	// fails — the load-bearing step of owner bundle 0006.
	if err := ownerPool.Runner.Exec(ctx,
		"ALTER TABLE striatumd.owner_transfer_probe OWNER TO CURRENT_USER"); err != nil {
		t.Fatalf("transfer probe ownership: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx,
		"REVOKE SELECT ON striatumd.owner_transfer_probe FROM striatumd_rw"); err != nil {
		t.Fatalf("re-close probe SELECT after transfer: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx,
		"REVOKE CREATE ON SCHEMA striatumd FROM striatumd_rw"); err != nil {
		t.Fatalf("revoke probe-scaffolding schema CREATE: %v", err)
	}
	err := runtimePool.Runner.Exec(ctx,
		"GRANT SELECT ON striatumd.owner_transfer_probe TO striatumd_rw")
	if err == nil {
		t.Fatal("runtime self-re-grant succeeded after ownership transfer; the revoke is not a boundary")
	}
	if code := pgErrCode(err); code != "42501" {
		t.Fatalf("post-transfer self-re-grant err=%v code=%q, want SQLSTATE 42501", err, code)
	}
	if scalar(t, ctx, ownerPool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_transfer_probe', 'SELECT')::text") != "false" {
		t.Fatal("probe SELECT reopened despite the failed re-grant")
	}
}

// TestIdentityGateTablesNotRuntimeOwned pins the post-bundle ownership posture
// for the three RFC 0114 tables. Vacuously green in pgtest (the pool user owns
// them before and after); the live-production equivalent is the doctor
// ownership probe, and the transfer mechanism itself is pinned by
// TestOwnerTransferClosesSelfRegrant.
func TestIdentityGateTablesNotRuntimeOwned(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	for _, table := range []string{"principals", "principal_clients", "client_sessions"} {
		if scalar(t, ctx, pool.Runner,
			"SELECT (tableowner <> 'striatumd_rw')::text FROM pg_tables WHERE schemaname = 'striatumd' AND tablename = $1", table) != "true" {
			t.Fatalf("striatumd.%s is still owned by striatumd_rw after owner bundle 0006", table)
		}
	}
}
