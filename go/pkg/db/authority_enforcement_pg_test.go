package db_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5/pgconn"
)

// enforcementDB applies the production owner bundle to a migrated database and
// returns the owner pool + ctx. The runtime role striatumd_rw is cluster-wide
// and not administrable by the (non-superuser) test owner, so enforcement is
// asserted via has_table_privilege (PostgreSQL's own privilege oracle) and via
// the role-agnostic authority gate — not by SET ROLE.
func enforcementDB(t *testing.T) (*db.Pool, context.Context) {
	t.Helper()
	pool := pgtest.Pool(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}
	return pool, ctx
}

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func scalar(t *testing.T, ctx context.Context, runner db.Runner, sql string, args ...any) string {
	t.Helper()
	v, err := runner.QueryScalar(ctx, sql, args...)
	if err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return v
}

func hasInsert(t *testing.T, ctx context.Context, runner db.Runner, table string) bool {
	t.Helper()
	return scalar(t, ctx, runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'INSERT')::text", table) == "true"
}

func hasColumnSelect(t *testing.T, ctx context.Context, runner db.Runner, table, column string) bool {
	t.Helper()
	return scalar(t, ctx, runner,
		"SELECT has_column_privilege('striatumd_rw', 'striatumd.'||$1, $2, 'SELECT')::text", table, column) == "true"
}

// TestParityReadGrant is the runtime-role read gate for the capability-parity
// check (RFC 0110 §8.2, owner bundle 0002). db.VerifyCapabilityParity reads
// striatumd.schema_authority AS THE RUNTIME ROLE at startup, so the runtime role
// must hold SELECT on it once any bundle is applied; otherwise the daemon fails
// parity with 42501 and crash-loops under Restart=on-failure. Bundle 0001
// created schema_authority owner-only and omitted the read grant; the slice-1
// parity tests ran as the PEER owner and could not catch the runtime-role gap.
// The read grant must not leak write privilege (schema_authority stays
// write-owner-only, RFC 0110 §13).
func TestParityReadGrant(t *testing.T) {
	pool, ctx := enforcementDB(t)
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.schema_authority', 'SELECT')::text") != "true" {
		t.Fatal("striatumd_rw cannot SELECT schema_authority; the startup capability-parity check would fail 42501 (owner bundle must GRANT SELECT)")
	}
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.schema_authority', 'INSERT')::text") != "false" {
		t.Fatal("striatumd_rw must not hold INSERT on schema_authority; write stays owner-only")
	}
}

// TestOwnerBundleWatermarkReadGrant is the runtime-role read gate for the RFC
// 0142 Layer 2 owner-bundle watermark interlock. db.CheckOwnerBundleWatermark
// reads striatumd.owner_bundle_meta AS THE RUNTIME ROLE before runtime
// migrations, so owner-DDL repair must leave a SELECT grant while preserving the
// owner-only write boundary.
func TestOwnerBundleWatermarkReadGrant(t *testing.T) {
	pool, ctx := enforcementDB(t)
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_bundle_meta', 'SELECT')::text") != "true" {
		t.Fatal("striatumd_rw cannot SELECT owner_bundle_meta; the owner-bundle watermark check would fail 42501 after owner-DDL repair")
	}
	for _, privilege := range []string{"INSERT", "UPDATE", "DELETE"} {
		if scalar(t, ctx, pool.Runner,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_bundle_meta', $1)::text", privilege) != "false" {
			t.Fatalf("striatumd_rw must not hold %s on owner_bundle_meta; write stays owner-only", privilege)
		}
	}
}

// TestPhase0DirectInsertDenied is T-42501-P0 + T-GRANT-DRIFT: after the runtime
// role is provisioned broad DML (as daemon doctor does), the owner bundle's
// REVOKE removes audit_log INSERT (only) — so a direct write is denied while
// unprotected surfaces keep their grant; and a stray re-grant is undone by
// ReassertWriteRevokes.
func TestPhase0DirectInsertDenied(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	// Provision broad DML first (mirrors daemon doctor --provision-rw-role), so
	// the post-bundle denial proves the bundle's REVOKE, not an absent grant.
	if err := pool.Runner.Exec(ctx, "GRANT USAGE ON SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision usage: %v", err)
	}
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON ALL TABLES IN SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision insert: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "audit_log") {
		t.Fatal("precondition: striatumd_rw should hold audit_log INSERT before the bundle")
	}

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	if hasInsert(t, ctx, pool.Runner, "audit_log") {
		t.Fatal("T-42501-P0: bundle did not revoke striatumd_rw audit_log INSERT")
	}
	// jobs is runtime coordination state (never SD-gated at any phase); it proves
	// the revoke is targeted, not a blanket strip (false-green guard).
	if !hasInsert(t, ctx, pool.Runner, "jobs") {
		t.Fatal("jobs INSERT must stay untouched (false-green guard)")
	}

	// T-GRANT-DRIFT: a stray re-grant reopens the hole; reassert closes it.
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON striatumd.audit_log TO striatumd_rw"); err != nil {
		t.Fatalf("simulate grant drift: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "audit_log") {
		t.Fatal("drift setup failed: re-grant did not take")
	}
	if err := db.ReassertWriteRevokes(ctx, pool.Runner); err != nil {
		t.Fatalf("reassert revokes: %v", err)
	}
	if hasInsert(t, ctx, pool.Runner, "audit_log") {
		t.Fatal("T-GRANT-DRIFT: ReassertWriteRevokes did not re-close audit_log INSERT")
	}
}

// TestExecAuthorityGate is T-EXEC-AUTH: append_audit_row with no/wrong daemon
// authority secret raises SQLSTATE 28000 and mutates zero rows; with the correct
// registered secret it succeeds and writes a v3 row. The gate is role-agnostic
// (the secret is authority), so it is exercised as the owner here.
func TestExecAuthorityGate(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-authority"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-test', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}

	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	before := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.audit_log")

	const call = `SELECT striatumd.append_audit_row('v','c','r','m','allowed',NULL,'rpc','q',true,'p')`

	// No secret -> 28000, zero rows.
	_, err = conn.Exec(ctx, call)
	if code := pgErrCode(err); code != "28000" {
		t.Fatalf("append without secret: got %v (code %q); want 28000", err, code)
	}
	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.audit_log"); got != before {
		t.Fatalf("failed authority append mutated rows: before=%s after=%s", before, got)
	}

	// Wrong secret -> 28000.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', 'wrong', false)"); err != nil {
		t.Fatalf("set wrong secret: %v", err)
	}
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "28000" {
		t.Fatalf("append with wrong secret: want 28000")
	}

	// Correct secret -> succeeds, writes v3 rows on the linear audit chain.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set correct secret: %v", err)
	}
	var auditID1, auditID2 int64
	if err := conn.QueryRow(ctx, call).Scan(&auditID1); err != nil {
		t.Fatalf("first authorized append failed: %v", err)
	}
	if err := conn.QueryRow(ctx, call).Scan(&auditID2); err != nil {
		t.Fatalf("second authorized append failed: %v", err)
	}
	if auditID1 == 0 || auditID2 == 0 || auditID1 == auditID2 {
		t.Fatalf("expected two distinct non-zero audit ids, got %d and %d", auditID1, auditID2)
	}
	format := scalar(t, ctx, pool.Runner,
		"SELECT COUNT(*)::text FROM striatumd.audit_log WHERE audit_id IN ($1,$2) AND hash_format_version = 3",
		auditID1, auditID2)
	if format != "2" {
		t.Fatalf("authorized appends wrote v3 count=%s; want 2", format)
	}
	hash1 := scalar(t, ctx, pool.Runner,
		"SELECT row_hash FROM striatumd.audit_log WHERE audit_id=$1", auditID1)
	prev2 := scalar(t, ctx, pool.Runner,
		"SELECT COALESCE(previous_hash,'') FROM striatumd.audit_log WHERE audit_id=$1", auditID2)
	if hash1 == "" {
		t.Fatal("first audit row has empty row_hash")
	}
	if prev2 != hash1 {
		t.Fatalf("audit chain broken: audit %d previous_hash=%q, want first row_hash=%q", auditID2, prev2, hash1)
	}
	headHash := scalar(t, ctx, pool.Runner,
		"SELECT last_hash FROM striatumd.audit_chain_head WHERE singleton = true")
	hash2 := scalar(t, ctx, pool.Runner,
		"SELECT row_hash FROM striatumd.audit_log WHERE audit_id=$1", auditID2)
	if headHash != hash2 {
		t.Fatalf("audit chain head last_hash=%q does not point at tail row_hash=%q", headHash, hash2)
	}
	if got := scalar(t, ctx, pool.Runner,
		"SELECT COUNT(*)::text FROM striatumd.audit_log WHERE audit_id IN ($1,$2) AND lock_wait_us IS NOT NULL AND lock_wait_us >= 0",
		auditID1, auditID2); got != "2" {
		t.Fatalf("audit SD appends with nonnegative lock_wait_us = %s; want 2", got)
	}
}

func mustExecErr(conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, ctx context.Context, sql string) error {
	_, err := conn.Exec(ctx, sql)
	return err
}

// TestPhase1ArtifactInsertDenied is T-42501-P1 + T-GRANT-DRIFT for the artifacts
// surface: owner bundle 0003 revokes striatumd_rw's direct artifacts INSERT (only),
// grants EXECUTE on the SD append function, and a stray re-grant is undone by
// ReassertWriteRevokes — while events (closed only at P2) keeps its INSERT.
func TestPhase1ArtifactInsertDenied(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if err := pool.Runner.Exec(ctx, "GRANT USAGE ON SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision usage: %v", err)
	}
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON ALL TABLES IN SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision insert: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "artifacts") {
		t.Fatal("precondition: striatumd_rw should hold artifacts INSERT before the bundle")
	}

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	if hasInsert(t, ctx, pool.Runner, "artifacts") {
		t.Fatal("T-42501-P1: bundle 0003 did not revoke striatumd_rw artifacts INSERT")
	}
	if !hasInsert(t, ctx, pool.Runner, "jobs") {
		t.Fatal("jobs INSERT must stay untouched (false-green guard)")
	}
	// The runtime role must hold EXECUTE on the sanctioned SD write path.
	if scalar(t, ctx, pool.Runner,
		"SELECT has_function_privilege('striatumd_rw', p.oid, 'EXECUTE')::text FROM pg_proc p WHERE proname='append_artifact_row' AND pronamespace='striatumd'::regnamespace ORDER BY oid DESC LIMIT 1") != "true" {
		t.Fatal("striatumd_rw lacks EXECUTE on append_artifact_row; P1 publishes would fail")
	}

	// T-GRANT-DRIFT: a stray re-grant reopens the hole; reassert closes it.
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON striatumd.artifacts TO striatumd_rw"); err != nil {
		t.Fatalf("simulate grant drift: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "artifacts") {
		t.Fatal("drift setup failed: re-grant did not take")
	}
	if err := db.ReassertWriteRevokes(ctx, pool.Runner); err != nil {
		t.Fatalf("reassert revokes: %v", err)
	}
	if hasInsert(t, ctx, pool.Runner, "artifacts") {
		t.Fatal("T-GRANT-DRIFT: ReassertWriteRevokes did not re-close artifacts INSERT")
	}
	// ReassertWriteRevokes must not over-reach: jobs (no stamp) keeps its grant.
	if !hasInsert(t, ctx, pool.Runner, "jobs") {
		t.Fatal("ReassertWriteRevokes over-revoked jobs (only stamped surfaces should close)")
	}
}

// TestArtifactExecAuthorityGate is T-EXEC-AUTH for the artifacts surface:
// append_artifact_row with no/wrong daemon-authority secret raises SQLSTATE 28000
// before the INSERT and mutates zero rows; with the correct secret the call gets
// PAST the authority gate to the INSERT (which then trips a foreign-key violation,
// 23503, because no parent repository/run is seeded — proving authority passed,
// not that the write was blocked).
func TestArtifactExecAuthorityGate(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-artifact-authority"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-test', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}

	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	before := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.artifacts")
	const call = `SELECT striatumd.append_artifact_row('repo','art','run',NULL,NULL,'log','finding','p/f.md','sha',10,'create',now(),NULL,NULL,NULL,NULL,1)`

	// No secret -> 28000, zero rows.
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "28000" {
		t.Fatalf("append without secret: want 28000, got %q", code)
	}
	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.artifacts"); got != before {
		t.Fatalf("failed authority append mutated rows: before=%s after=%s", before, got)
	}

	// Wrong secret -> 28000.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', 'wrong', false)"); err != nil {
		t.Fatalf("set wrong secret: %v", err)
	}
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "28000" {
		t.Fatalf("append with wrong secret: want 28000, got %q", code)
	}

	// Correct secret -> authority passes, INSERT reached: FK violation (23503),
	// not an authority denial. Zero artifact rows still land.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set correct secret: %v", err)
	}
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "23503" {
		t.Fatalf("authorized append: want 23503 (FK violation past the authority gate), got %q", code)
	}
	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.artifacts"); got != before {
		t.Fatalf("authority gate let a partial row land: before=%s after=%s", before, got)
	}
}

// TestPhase2EventInsertDenied is T-42501-P2 + T-GRANT-DRIFT for the events
// surface: owner bundle 0004 revokes striatumd_rw's direct events INSERT (only),
// grants EXECUTE on the SD append function, and a stray re-grant is undone by
// ReassertWriteRevokes — while jobs (never SD-gated) keeps its INSERT. This is
// the last L1 phase: all three durable append-only surfaces are now SD-only.
func TestPhase2EventInsertDenied(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()

	if err := pool.Runner.Exec(ctx, "GRANT USAGE ON SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision usage: %v", err)
	}
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON ALL TABLES IN SCHEMA striatumd TO striatumd_rw"); err != nil {
		t.Fatalf("provision insert: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "events") {
		t.Fatal("precondition: striatumd_rw should hold events INSERT before the bundle")
	}

	if _, _, err := db.ApplyOwnerBundles(ctx, pool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	if hasInsert(t, ctx, pool.Runner, "events") {
		t.Fatal("T-42501-P2: bundle 0004 did not revoke striatumd_rw events INSERT")
	}
	if !hasInsert(t, ctx, pool.Runner, "jobs") {
		t.Fatal("jobs INSERT must stay untouched (false-green guard)")
	}
	// The runtime role must hold EXECUTE on the sanctioned SD write path.
	if scalar(t, ctx, pool.Runner,
		"SELECT has_function_privilege('striatumd_rw', p.oid, 'EXECUTE')::text FROM pg_proc p WHERE proname='append_event_row' AND pronamespace='striatumd'::regnamespace ORDER BY oid DESC LIMIT 1") != "true" {
		t.Fatal("striatumd_rw lacks EXECUTE on append_event_row; P2 event appends would fail")
	}

	// T-GRANT-DRIFT: a stray re-grant reopens the hole; reassert closes it.
	if err := pool.Runner.Exec(ctx, "GRANT INSERT ON striatumd.events TO striatumd_rw"); err != nil {
		t.Fatalf("simulate grant drift: %v", err)
	}
	if !hasInsert(t, ctx, pool.Runner, "events") {
		t.Fatal("drift setup failed: re-grant did not take")
	}
	if err := db.ReassertWriteRevokes(ctx, pool.Runner); err != nil {
		t.Fatalf("reassert revokes: %v", err)
	}
	if hasInsert(t, ctx, pool.Runner, "events") {
		t.Fatal("T-GRANT-DRIFT: ReassertWriteRevokes did not re-close events INSERT")
	}
}

// TestEventExecAuthorityGate is T-EXEC-AUTH for the events surface:
// append_event_row with no/wrong daemon-authority secret raises SQLSTATE 28000
// before any work and mutates zero rows; with the correct secret the call gets
// PAST the authority gate (and the empty-payload transcript check) to the INSERT,
// which trips a foreign-key violation (23503) because no parent repository is
// seeded — proving authority passed, not that the write was blocked.
func TestEventExecAuthorityGate(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-event-authority"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-test', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}

	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	before := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.events")
	const call = `SELECT striatumd.append_event_row('repo',NULL,'job.created',NULL,NULL,NULL,NULL,NULL,'{}'::jsonb)`

	// No secret -> 28000, zero rows.
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "28000" {
		t.Fatalf("append without secret: want 28000, got %q", code)
	}
	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.events"); got != before {
		t.Fatalf("failed authority append mutated rows: before=%s after=%s", before, got)
	}

	// Wrong secret -> 28000.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', 'wrong', false)"); err != nil {
		t.Fatalf("set wrong secret: %v", err)
	}
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "28000" {
		t.Fatalf("append with wrong secret: want 28000, got %q", code)
	}

	// Correct secret -> authority passes, INSERT reached: FK violation (23503),
	// not an authority denial. Zero event rows still land.
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set correct secret: %v", err)
	}
	if code := pgErrCode(mustExecErr(conn, ctx, call)); code != "23503" {
		t.Fatalf("authorized append: want 23503 (FK violation past the authority gate), got %q", code)
	}
	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.events"); got != before {
		t.Fatalf("authority gate let a partial row land: before=%s after=%s", before, got)
	}
}

// TestEventTranscriptExclusion is T-EVENT-NOTRANSCRIPT (RFC 0110 §12,
// C-EVENT-NO-TRANSCRIPTS): with valid daemon authority, a payload carrying a
// top-level transcript key OR a transcript-sized payload is DB-rejected (23514,
// distinct from the FK code) and mutates zero rows; a clean payload passes the
// transcript gate and reaches the INSERT (23503 FK, no parent repo) — proving the
// rejection is the transcript check, not a blanket refusal.
func TestEventTranscriptExclusion(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-event-transcript"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-test', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}

	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	before := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.events")
	const callTmpl = `SELECT striatumd.append_event_row('repo',NULL,'lane.output',NULL,NULL,NULL,NULL,NULL,$1::jsonb)`

	// Each forbidden top-level key is rejected with 23514, zero rows.
	for _, key := range []string{"stdout", "stderr", "transcript", "raw_output", "provider_output"} {
		payload := fmt.Sprintf(`{%q: "captured agent output"}`, key)
		_, err := conn.Exec(ctx, callTmpl, payload)
		if code := pgErrCode(err); code != "23514" {
			t.Fatalf("forbidden key %q: want 23514 (transcript exclusion), got %q (%v)", key, code, err)
		}
	}

	// A transcript-sized payload (> 256 KiB) is rejected with 23514, zero rows.
	big := `{"data":"` + strings.Repeat("x", 300*1024) + `"}`
	if code := pgErrCode(mustExecErrArg(conn, ctx, callTmpl, big)); code != "23514" {
		t.Fatalf("oversize payload: want 23514, got %q", code)
	}

	if got := scalar(t, ctx, pool.Runner, "SELECT COUNT(*)::text FROM striatumd.events"); got != before {
		t.Fatalf("rejected payloads mutated rows: before=%s after=%s", before, got)
	}

	// A clean payload passes the transcript gate and reaches the INSERT (23503 FK,
	// no parent repo) — not a transcript rejection.
	clean := `{"job_id":"job_1","state":"running"}`
	if code := pgErrCode(mustExecErrArg(conn, ctx, callTmpl, clean)); code != "23503" {
		t.Fatalf("clean payload: want 23503 (passed the transcript gate, hit FK), got %q", code)
	}
}

func mustExecErrArg(conn interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, ctx context.Context, sql string, arg any) error {
	_, err := conn.Exec(ctx, sql, arg)
	return err
}

// TestEventSDPathLandsAndChains is the positive end-to-end P2 proof: with a
// seeded repository (FK parent) and valid daemon authority, append_event_row
// lands a real events row entirely in-DB (assigning event_id, created_at, an
// in-DB v3 row_hash, and previous_hash) and advances the per-repository chain
// head; a second append links its previous_hash to the first row's row_hash, so
// the chain stays linear (assertEventChainLinear's invariant) across SD writes.
func TestEventSDPathLandsAndChains(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-event-positive"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-test', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.repositories(
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state)
		VALUES ('repo_p2', 'id', '/tmp/r', '/tmp/r/.striatum', 'r', now(), 1, 'active')`); err != nil {
		t.Fatalf("seed repository: %v", err)
	}

	conn, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}

	const call = `SELECT striatumd.append_event_row('repo_p2',NULL,$1,NULL,NULL,NULL,NULL,NULL,$2::jsonb)`

	var id1, id2 int64
	if err := conn.QueryRow(ctx, call, "run.started", `{"k":"v"}`).Scan(&id1); err != nil {
		t.Fatalf("first append failed: %v", err)
	}
	if err := conn.QueryRow(ctx, call, "job.created", `{"job":"j1"}`).Scan(&id2); err != nil {
		t.Fatalf("second append failed: %v", err)
	}
	if id1 == 0 || id2 == 0 || id1 == id2 {
		t.Fatalf("expected two distinct non-zero event ids, got %d and %d", id1, id2)
	}

	// Both rows landed with a v3 hash and link into one linear chain.
	hash1 := scalar(t, ctx, pool.Runner,
		"SELECT row_hash FROM striatumd.events WHERE repository_id='repo_p2' AND event_id=$1", id1)
	prev2 := scalar(t, ctx, pool.Runner,
		"SELECT COALESCE(previous_hash,'') FROM striatumd.events WHERE repository_id='repo_p2' AND event_id=$1", id2)
	if hash1 == "" {
		t.Fatal("first event has empty row_hash")
	}
	if prev2 != hash1 {
		t.Fatalf("chain broken: event %d previous_hash=%q, want first row_hash=%q", id2, prev2, hash1)
	}
	headHash := scalar(t, ctx, pool.Runner,
		"SELECT last_hash FROM striatumd.repo_event_chain_heads WHERE repository_id='repo_p2'")
	hash2 := scalar(t, ctx, pool.Runner,
		"SELECT row_hash FROM striatumd.events WHERE repository_id='repo_p2' AND event_id=$1", id2)
	if headHash != hash2 {
		t.Fatalf("chain head last_hash=%q does not point at the tail row_hash=%q", headHash, hash2)
	}
	if got := scalar(t, ctx, pool.Runner,
		"SELECT COUNT(*)::text FROM striatumd.events WHERE repository_id='repo_p2' AND event_id IN ($1,$2) AND lock_wait_us IS NOT NULL AND lock_wait_us >= 0",
		id1, id2); got != "2" {
		t.Fatalf("event SD appends with nonnegative lock_wait_us = %s; want 2", got)
	}
}

func TestAuditSDPathRecordsPositiveLockWaitUnderContention(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-audit-contention"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-audit-contention', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}

	locker, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire locker: %v", err)
	}
	defer locker.Release()
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatalf("begin locker tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, "SELECT last_hash FROM striatumd.audit_chain_head WHERE singleton = true FOR UPDATE"); err != nil {
		t.Fatalf("lock audit chain head: %v", err)
	}

	appender, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire appender: %v", err)
	}
	defer appender.Release()
	if _, err := appender.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	pid := backendPID(t, ctx, appender)
	done := make(chan asyncAppendResult, 1)
	go func() {
		var auditID int64
		err := appender.QueryRow(ctx,
			`SELECT striatumd.append_audit_row('v','c','r','m','allowed',NULL,'rpc','q-contention',true,'p')`).Scan(&auditID)
		done <- asyncAppendResult{id: auditID, err: err}
	}()

	waitForBackendLockWait(t, ctx, pool.Runner, pid, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit locker tx: %v", err)
	}
	committed = true
	result := waitForAsyncAppend(t, done)
	if result.id == 0 {
		t.Fatal("authorized audit append returned zero audit id")
	}
	if got := scalar(t, ctx, pool.Runner,
		"SELECT (lock_wait_us > 0)::text FROM striatumd.audit_log WHERE audit_id=$1",
		result.id); got != "true" {
		t.Fatalf("contended audit append lock_wait_us > 0 = %s; want true", got)
	}
}

func TestEventSDPathRecordsPositiveLockWaitUnderContention(t *testing.T) {
	pool, ctx := enforcementDB(t)
	const secret = "s3cr3t-event-contention"
	const salt = "per-instance-salt"
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-event-contention', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		secret, salt); err != nil {
		t.Fatalf("register secret: %v", err)
	}
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.repositories(
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state)
		VALUES ('repo_event_contention', 'id-event-contention', '/tmp/r-event-contention',
		        '/tmp/r-event-contention/.striatum', 'r-event-contention', now(), 1, 'active')`); err != nil {
		t.Fatalf("seed repository: %v", err)
	}

	appender, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire appender: %v", err)
	}
	defer appender.Release()
	if _, err := appender.Exec(ctx, "SELECT set_config('striatum.daemon_auth', $1, false)", secret); err != nil {
		t.Fatalf("set secret: %v", err)
	}
	const call = `SELECT striatumd.append_event_row('repo_event_contention',NULL,$1,NULL,NULL,NULL,NULL,NULL,$2::jsonb)`
	var firstID int64
	if err := appender.QueryRow(ctx, call, "run.started", `{"k":"v"}`).Scan(&firstID); err != nil {
		t.Fatalf("first append failed: %v", err)
	}

	locker, err := pool.RawPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire locker: %v", err)
	}
	defer locker.Release()
	tx, err := locker.Begin(ctx)
	if err != nil {
		t.Fatalf("begin locker tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, "SELECT last_hash FROM striatumd.repo_event_chain_heads WHERE repository_id='repo_event_contention' FOR UPDATE"); err != nil {
		t.Fatalf("lock event chain head: %v", err)
	}

	pid := backendPID(t, ctx, appender)
	done := make(chan asyncAppendResult, 1)
	go func() {
		var eventID int64
		err := appender.QueryRow(ctx, call, "job.created", `{"job":"j1"}`).Scan(&eventID)
		done <- asyncAppendResult{id: eventID, err: err}
	}()

	waitForBackendLockWait(t, ctx, pool.Runner, pid, done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit locker tx: %v", err)
	}
	committed = true
	result := waitForAsyncAppend(t, done)
	if result.id == 0 || result.id == firstID {
		t.Fatalf("authorized event append returned invalid event id %d after first id %d", result.id, firstID)
	}
	if got := scalar(t, ctx, pool.Runner,
		"SELECT (lock_wait_us > 0)::text FROM striatumd.events WHERE repository_id='repo_event_contention' AND event_id=$1",
		result.id); got != "true" {
		t.Fatalf("contended event append lock_wait_us > 0 = %s; want true", got)
	}
}

type asyncAppendResult struct {
	id  int64
	err error
}

func backendPID(t *testing.T, ctx context.Context, conn interface {
	QueryRow(context.Context, string, ...any) db.Row
}) int {
	t.Helper()
	var pid int
	if err := conn.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("read backend pid: %v", err)
	}
	return pid
}

func waitForBackendLockWait(t *testing.T, ctx context.Context, runner db.Runner, pid int, done <-chan asyncAppendResult) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case result := <-done:
			t.Fatalf("append returned before observing lock wait: id=%d err=%v", result.id, result.err)
		default:
		}
		if got := scalar(t, ctx, runner,
			`SELECT EXISTS (
			   SELECT 1
			     FROM pg_stat_activity
			    WHERE pid = $1
			      AND wait_event_type = 'Lock'
			 )::text`, pid); got == "true" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backend %d did not enter PostgreSQL lock wait", pid)
}

func waitForAsyncAppend(t *testing.T, done <-chan asyncAppendResult) asyncAppendResult {
	t.Helper()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("authorized append failed: %v", result.err)
		}
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for authorized append")
		return asyncAppendResult{}
	}
}

// TestRegistryACL is T-REGISTRY-ACL: the runtime role holds no SELECT on the
// owner-only authority registry.
func TestRegistryACL(t *testing.T) {
	pool, ctx := enforcementDB(t)
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.daemon_auth_registry', 'SELECT')::text") != "false" {
		t.Fatal("striatumd_rw can SELECT daemon_auth_registry; must be owner-only")
	}
}

// TestTokenSecretColumnsUseAuthorityProjection is the #164/RFC 0113 R1
// behavior gate: owner bundle 0005 removes direct runtime SELECT on token
// hashes/salts while preserving daemon-authorized token validation.
func TestTokenSecretColumnsUseAuthorityProjection(t *testing.T) {
	ownerPool, runtimePool := pgtest.Pools(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, ownerPool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	const authoritySecret = "projection-secret"
	const authoritySalt = "projection-salt"
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-projection', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		authoritySecret, authoritySalt); err != nil {
		t.Fatalf("register authority secret: %v", err)
	}

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ('repo_projection','ident_projection','/tmp/repo-projection',
		          '/tmp/repo-projection/.striatum','repo-projection',$1,23,'active')`,
		now); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
	const tokenSalt = "token-salt"
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.clients (
		  client_id, client_kind, display_name, token_id, token_hash, token_salt,
		  created_at
		) VALUES ('client_projection','test','projection client','tok_projection',$1,$2,$3)`,
		hmacHex(tokenSalt, "secret"), tokenSalt, now); err != nil {
		t.Fatalf("insert client: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.client_capabilities (
		  capability_id, client_id, repository_id, capability, granted_at, session_id
		) VALUES ('cap_projection','client_projection','repo_projection',$1,$2,'sess_projection')`,
		string(rpc.CapabilityRead), now); err != nil {
		t.Fatalf("insert capability: %v", err)
	}

	if hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "token_hash") ||
		hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "token_salt") {
		t.Fatal("striatumd_rw can SELECT clients token secret columns after owner bundle 0005")
	}
	if !hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "client_id") {
		t.Fatal("striatumd_rw lost clients.client_id SELECT; only token secret columns should be denied")
	}
	err := runtimePool.Runner.Exec(ctx,
		"SELECT * FROM striatumd.clients WHERE token_id = $1", "tok_projection")
	if code := pgErrCode(err); code != "42501" {
		t.Fatalf("runtime SELECT * from clients got err=%v code=%q, want SQLSTATE 42501", err, code)
	}
	var tokenID string
	if err := runtimePool.Runner.QueryRow(ctx,
		"SELECT token_id FROM striatumd.clients WHERE token_id = $1", "tok_projection").Scan(&tokenID); err != nil {
		t.Fatalf("runtime role cannot read non-secret token metadata: %v", err)
	}
	var tokenHash string
	err = runtimePool.Runner.QueryRow(ctx,
		"SELECT token_hash FROM striatumd.clients WHERE token_id = $1", "tok_projection").Scan(&tokenHash)
	if code := pgErrCode(err); code != "42501" {
		t.Fatalf("runtime direct token_hash read got err=%v code=%q, want SQLSTATE 42501", err, code)
	}
	err = runtimePool.Runner.QueryRow(ctx,
		"SELECT token_hash FROM striatumd.load_token_for_update($1)", "tok_projection").Scan(&tokenHash)
	if code := pgErrCode(err); code != "28000" {
		t.Fatalf("projection without daemon authority got err=%v code=%q, want SQLSTATE 28000", err, code)
	}

	required := rpc.CapabilityRead
	auth := rpc.PostgresAuthorizer{
		Runner:          runtimePool.Runner,
		Clock:           func() time.Time { return now },
		AuthoritySecret: authoritySecret,
	}
	allowed := auth.Authorize(&required, "repo_projection", "tok_projection.secret")
	if allowed.Decision != "allowed" || allowed.ClientID != "client_projection" ||
		allowed.SessionID != "sess_projection" || allowed.Capability != rpc.CapabilityRead {
		t.Fatalf("authorized projection auth = %#v, want allowed read for client_projection/sess_projection", allowed)
	}
	denied := auth.Authorize(&required, "repo_projection", "tok_projection.wrong")
	if denied.Decision != "denied" || denied.DenialReason != "token_invalid" {
		t.Fatalf("wrong-secret projection auth = %#v, want denied/token_invalid", denied)
	}

	if err := ownerPool.Runner.Exec(ctx, "GRANT SELECT (token_hash, token_salt) ON striatumd.clients TO striatumd_rw"); err != nil {
		t.Fatalf("simulate read grant drift: %v", err)
	}
	if !hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "token_hash") {
		t.Fatal("read grant drift setup failed: token_hash SELECT did not reopen")
	}
	if err := db.ReassertReadRevokes(ctx, ownerPool.Runner); err != nil {
		t.Fatalf("reassert read revokes: %v", err)
	}
	if hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "token_hash") ||
		hasColumnSelect(t, ctx, ownerPool.Runner, "clients", "token_salt") {
		t.Fatal("ReassertReadRevokes did not re-close clients token secret columns")
	}
}

// TestPrincipalProjectionsRequireDaemonAuthority pins the RFC 0114 projection
// gate: each owner bundle 0006 SECURITY DEFINER projection raises through
// striatumd.assert_daemon_authority() under a wrong or absent secret (SQLSTATE
// 28000) and returns rows under the correct one, called as the runtime role.
func TestPrincipalProjectionsRequireDaemonAuthority(t *testing.T) {
	ownerPool, runtimePool := pgtest.Pools(t)
	ctx := context.Background()
	if _, _, err := db.ApplyOwnerBundles(ctx, ownerPool.Runner, "test"); err != nil {
		t.Fatalf("apply owner bundle: %v", err)
	}

	const authoritySecret = "identity-projection-secret"
	const authoritySalt = "identity-projection-salt"
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.daemon_auth_registry(instance_id, role_name, digest, salt)
		VALUES ('inst-identity', 'striatumd_rw',
		        encode(striatumd.digest(convert_to($1 || $2, 'UTF8'), 'sha256'), 'hex'), $2)`,
		authoritySecret, authoritySalt); err != nil {
		t.Fatalf("register authority secret: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.principals(principal_id, principal_kind, display_name, created_at)
		VALUES ('prin_proj', 'human', 'projection person', now())`); err != nil {
		t.Fatalf("seed principal: %v", err)
	}
	if err := ownerPool.Runner.Exec(ctx, `
		INSERT INTO striatumd.principal_clients(principal_id, client_id, linked_at)
		VALUES ('prin_proj', 'client_proj', now())`); err != nil {
		t.Fatalf("seed principal link: %v", err)
	}

	calls := []struct {
		name string
		sql  string
		args []any
	}{
		{"get_principal", "SELECT principal_id FROM striatumd.get_principal($1, $2)", []any{"prin_proj"}},
		{"resolve_principal_for_client", "SELECT principal_id FROM striatumd.resolve_principal_for_client($1, $2)", []any{"client_proj"}},
		{"list_principal_scopes", "SELECT striatumd.list_principal_scopes($1)", nil},
		{"link_client_to_principal", "SELECT striatumd.link_client_to_principal($1, $2, $3)", []any{"prin_proj", "client_proj"}},
	}
	for _, call := range calls {
		for _, secret := range []string{"", "wrong-secret"} {
			args := append([]any{secret}, call.args...)
			var value string
			err := runtimePool.Runner.QueryRow(ctx, call.sql, args...).Scan(&value)
			if code := pgErrCode(err); code != "28000" {
				t.Fatalf("%s with secret %q got err=%v code=%q, want SQLSTATE 28000", call.name, secret, err, code)
			}
		}
	}

	var principalID string
	if err := runtimePool.Runner.QueryRow(ctx,
		"SELECT principal_id FROM striatumd.get_principal($1, $2)",
		authoritySecret, "prin_proj").Scan(&principalID); err != nil || principalID != "prin_proj" {
		t.Fatalf("authorized get_principal = (%q, %v), want prin_proj", principalID, err)
	}
	if err := runtimePool.Runner.QueryRow(ctx,
		"SELECT principal_id FROM striatumd.resolve_principal_for_client($1, $2)",
		authoritySecret, "client_proj").Scan(&principalID); err != nil || principalID != "prin_proj" {
		t.Fatalf("authorized resolve_principal_for_client = (%q, %v), want prin_proj", principalID, err)
	}
	var payload string
	if err := runtimePool.Runner.QueryRow(ctx,
		"SELECT striatumd.list_principal_scopes($1)", authoritySecret).Scan(&payload); err != nil {
		t.Fatalf("authorized list_principal_scopes: %v", err)
	}
	if !strings.Contains(payload, "prin_proj") {
		t.Fatalf("list_principal_scopes payload %q does not contain the seeded principal", payload)
	}
	if err := runtimePool.Runner.Exec(ctx,
		"SELECT striatumd.link_client_to_principal($1, $2, $3)",
		authoritySecret, "prin_proj", "client_proj"); err != nil {
		t.Fatalf("authorized link_client_to_principal: %v", err)
	}
}

// TestReassertReadRevokesReclosesIdentitySelect extends the bundle 0005 drift
// test to the RFC 0114 surfaces: an owner re-grant of broad SELECT on
// principals / principal_clients / client_sessions is re-closed by
// ReassertReadRevokes — which still re-closes the 0005 clients columns too.
func TestReassertReadRevokesReclosesIdentitySelect(t *testing.T) {
	pool, ctx := enforcementDB(t)

	for _, stmt := range []string{
		"GRANT SELECT ON striatumd.principals TO striatumd_rw",
		"GRANT SELECT ON striatumd.principal_clients TO striatumd_rw",
		"GRANT SELECT ON striatumd.client_sessions TO striatumd_rw",
		"GRANT SELECT (token_hash, token_salt) ON striatumd.clients TO striatumd_rw",
		"REVOKE SELECT ON striatumd.owner_bundle_meta FROM striatumd_rw",
	} {
		if err := pool.Runner.Exec(ctx, stmt); err != nil {
			t.Fatalf("simulate read grant drift (%q): %v", stmt, err)
		}
	}
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.principals', 'SELECT')::text") != "true" {
		t.Fatal("read grant drift setup failed: principals SELECT did not reopen")
	}
	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_bundle_meta', 'SELECT')::text") != "false" {
		t.Fatal("read grant drift setup failed: owner_bundle_meta SELECT did not close")
	}

	if err := db.ReassertReadRevokes(ctx, pool.Runner); err != nil {
		t.Fatalf("reassert read revokes: %v", err)
	}

	if scalar(t, ctx, pool.Runner,
		"SELECT has_table_privilege('striatumd_rw', 'striatumd.owner_bundle_meta', 'SELECT')::text") != "true" {
		t.Fatal("ReassertReadRevokes stranded the owner_bundle_meta startup watermark SELECT grant")
	}
	for _, table := range []string{"principals", "client_sessions"} {
		if scalar(t, ctx, pool.Runner,
			"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, 'SELECT')::text", table) != "false" {
			t.Fatalf("ReassertReadRevokes did not re-close %s SELECT", table)
		}
	}
	if hasColumnSelect(t, ctx, pool.Runner, "principal_clients", "principal_id") {
		t.Fatal("ReassertReadRevokes did not re-close principal_clients.principal_id")
	}
	if !hasColumnSelect(t, ctx, pool.Runner, "principal_clients", "client_id") {
		t.Fatal("ReassertReadRevokes stranded the principal_clients.client_id grant-back")
	}
	if hasColumnSelect(t, ctx, pool.Runner, "clients", "token_hash") ||
		hasColumnSelect(t, ctx, pool.Runner, "clients", "token_salt") {
		t.Fatal("ReassertReadRevokes no longer re-closes the bundle 0005 clients token columns")
	}
	for _, table := range []string{"principals", "principal_clients", "client_sessions"} {
		for _, privilege := range []string{"INSERT", "UPDATE", "DELETE"} {
			if scalar(t, ctx, pool.Runner,
				"SELECT has_table_privilege('striatumd_rw', 'striatumd.'||$1, $2)::text", table, privilege) != "true" {
				t.Fatalf("ReassertReadRevokes stranded %s %s; the DML grant-backs must be restated", table, privilege)
			}
		}
	}
}

// TestSDHardening is T-SD-HARDEN: the owner-owned write functions are SECURITY
// DEFINER with a pinned search_path and no PUBLIC execute.
func TestSDHardening(t *testing.T) {
	pool, ctx := enforcementDB(t)
	for _, fn := range []string{"assert_daemon_authority", "append_audit_row", "append_artifact_row", "append_event_row"} {
		if scalar(t, ctx, pool.Runner,
			"SELECT prosecdef::text FROM pg_proc WHERE proname=$1 AND pronamespace='striatumd'::regnamespace ORDER BY oid DESC LIMIT 1", fn) != "true" {
			t.Errorf("%s is not SECURITY DEFINER", fn)
		}
		cfg := scalar(t, ctx, pool.Runner,
			"SELECT COALESCE(array_to_string(proconfig, ','), '') FROM pg_proc WHERE proname=$1 AND pronamespace='striatumd'::regnamespace ORDER BY oid DESC LIMIT 1", fn)
		if !containsAll(cfg, "search_path=", "striatumd", "pg_temp") {
			t.Errorf("%s search_path not pinned (proconfig=%q)", fn, cfg)
		}
		if scalar(t, ctx, pool.Runner,
			"SELECT has_function_privilege('public', oid, 'EXECUTE')::text FROM pg_proc WHERE proname=$1 AND pronamespace='striatumd'::regnamespace ORDER BY oid DESC LIMIT 1", fn) != "false" {
			t.Errorf("%s is executable by PUBLIC", fn)
		}
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func hmacHex(salt, secret string) string {
	mac := hmac.New(sha256.New, []byte(salt))
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}
