package db

// WriteAuthorityClass classifies how a daemon-owned table may be written, per
// the RFC 0110 §13 write-authority inventory. Every striatumd.* table carries a
// classification so a future table cannot silently bypass the L1 model; the
// inventory guard (TestWriteAuthorityInventoryComplete) fails when a live table
// has no row here.
type WriteAuthorityClass string

const (
	// ClassSDGated: writes go only through an owner-owned SECURITY DEFINER
	// function from the phase named in the comment (RFC 0110 §7).
	ClassSDGated WriteAuthorityClass = "sd_gated"
	// ClassRuntimeDML: direct runtime-role DML retained (live coordination
	// state); some are append-only via triggers (UPDATE/DELETE revoked).
	ClassRuntimeDML WriteAuthorityClass = "runtime_dml"
	// ClassOwnerOnly: the runtime role holds no write privilege (authority
	// schema + migration bookkeeping).
	ClassOwnerOnly WriteAuthorityClass = "owner_only"
)

// writeAuthorityInventory classifies every daemon-owned table at the current
// phase (Phase 2 = full). All three durable append-only surfaces (audit_log,
// artifacts, events) are SD-function-only.
var writeAuthorityInventory = map[string]WriteAuthorityClass{
	// Phase 0: audit_log is SD-function-only.
	"audit_log": ClassSDGated,
	// Phase 1 (audit_artifacts): artifacts is SD-function-only (owner bundle
	// 0003, append_artifact_row). It is append-only via triggers too.
	"artifacts": ClassSDGated,
	// Phase 2 (full): events is SD-function-only (owner bundle 0004,
	// append_event_row). It is append-only via triggers too.
	"events": ClassSDGated,

	// Authority schema + migration bookkeeping: owner-only.
	"daemon_auth_registry": ClassOwnerOnly,
	"daemon_auth_log":      ClassOwnerOnly,
	"schema_authority":     ClassOwnerOnly,
	"owner_bundle_meta":    ClassOwnerOnly,
	"schema_meta":          ClassOwnerOnly,
	"schema_migrations":    ClassOwnerOnly,
	"repo_migrations":      ClassOwnerOnly,
	"daemon_meta":          ClassOwnerOnly,

	// Live coordination + durable state: direct runtime DML retained for now.
	// audit_chain_head / repo_event_chain_heads are the chain head pointers the
	// SD append functions advance as owner; they stay runtime-writable (derived
	// pointers, not the protected append-only surfaces).
	"audit_chain_head":               ClassRuntimeDML,
	"audit_segments":                 ClassRuntimeDML,
	"audit_repositories":             ClassRuntimeDML,
	"apply_receipts":                 ClassRuntimeDML,
	"auto_finalize_circuit_breakers": ClassRuntimeDML,
	// barrier_staged_contributions (RFC 0135 P1, migration 0029): the
	// attempt-addressed fan-in staging rows. Direct runtime DML (a sibling
	// completion INSERTs a row; a requeue UPDATEs it to 'voided') — live
	// coordination state, not an SD-gated append surface.
	"barrier_staged_contributions": ClassRuntimeDML,
	// barrier_state (RFC 0135 P2, migration 0030): the recoverable barrier-assembly
	// journal. Direct runtime DML — the assembler INSERTs the two-phase intent and
	// UPDATEs the state through sealed -> assembling -> committed|failed; live
	// coordination state, not an SD-gated append surface.
	"barrier_state": ClassRuntimeDML,
	"blockers":      ClassRuntimeDML,
	// dissent_ledger (RFC 0135 P4, migration 0032): the forward-written panel-quorum
	// dissent witness. Runtime-writable INSERT but APPEND-ONLY via a refuse-trigger +
	// the SELECT/INSERT-only grant (UPDATE/DELETE revoked) — the same shape as
	// fanin_freeze_points above, NOT an SD-gated surface (it uses a plain
	// refuse-trigger, not the assert_daemon_authority gate). Written co-transactionally
	// with the verdict insert under the per-run advisory lock.
	"dissent_ledger":                 ClassRuntimeDML,
	"client_capabilities":            ClassRuntimeDML,
	"client_sessions":                ClassRuntimeDML,
	"clients":                        ClassRuntimeDML,
	"command_requests":               ClassRuntimeDML,
	"conversations":                  ClassRuntimeDML,
	"conversation_post_dialog_hooks": ClassRuntimeDML,
	"cross_repo_cycle_counters":      ClassRuntimeDML,
	"cross_repo_run_repositories":    ClassRuntimeDML,
	"cross_repo_runs":                ClassRuntimeDML,
	"daemon_supervisors":             ClassRuntimeDML,
	"escalation_inbox":               ClassRuntimeDML,
	// fanin_freeze_points (RFC 0135 P1, migration 0029): the immutable fan-in
	// freeze record. Runtime-writable INSERT but APPEND-ONLY via a refuse-trigger
	// + the SELECT/INSERT-only grant (UPDATE/DELETE revoked) — the same shape as
	// the other "append-only via triggers" runtime_dml tables, NOT an SD-gated
	// surface (it uses a plain refuse-trigger, not the assert_daemon_authority gate).
	"fanin_freeze_points":         ClassRuntimeDML,
	"interrogations":              ClassRuntimeDML,
	"job_dependencies":            ClassRuntimeDML,
	"job_recovery_state":          ClassRuntimeDML,
	"job_workspaces":              ClassRuntimeDML,
	"job_worktrees":               ClassRuntimeDML,
	"jobs":                        ClassRuntimeDML,
	"leases":                      ClassRuntimeDML,
	"principal_clients":           ClassRuntimeDML,
	"principals":                  ClassRuntimeDML,
	"process_executions":          ClassRuntimeDML,
	"process_supervisor_pointers": ClassRuntimeDML,
	"process_supervisors":         ClassRuntimeDML,
	"queue_messages":              ClassRuntimeDML,
	"repo_event_chain_heads":      ClassRuntimeDML,
	"repositories":                ClassRuntimeDML,
	"rpc_methods":                 ClassRuntimeDML,
	"rpc_request_log":             ClassRuntimeDML,
	"runs":                        ClassRuntimeDML,
	"scheduler_cursors":           ClassRuntimeDML,
	"sessions":                    ClassRuntimeDML,
	// spawn_authorization_grants (RFC 0122, migration 0027): the daemon
	// captures a grant at run.start and revokes it (UPDATE revoked_at) at run
	// terminal / run.stop. Live authorization state, direct runtime DML — like
	// leases/sessions, not an append-only provenance surface.
	"spawn_authorization_grants": ClassRuntimeDML,
	// supervisor_buffered_packets (#456 / FMA-006, migration 0038): the durable
	// buffer for no-reader supervised_push packets. The delivery path INSERTs a
	// buffered packet, the reader-attach replay DELETEs the drained rows, and a
	// post-restart hydrate SELECTs them — live coordination state, direct runtime
	// DML, not an append-only provenance surface.
	"supervisor_buffered_packets": ClassRuntimeDML,
	"trajectory_segments":         ClassRuntimeDML,
	"verdicts":                    ClassRuntimeDML,
	"work_packets":                ClassRuntimeDML,
	"workflow_accepted_risks":     ClassRuntimeDML,
	"workflow_snapshots":          ClassRuntimeDML,
}

// ClassifyTable returns the write-authority classification of a striatumd.*
// table and whether it is in the inventory.
func ClassifyTable(table string) (WriteAuthorityClass, bool) {
	class, ok := writeAuthorityInventory[table]
	return class, ok
}

// WriteAuthorityInventory returns a copy of the full classification.
func WriteAuthorityInventory() map[string]WriteAuthorityClass {
	out := make(map[string]WriteAuthorityClass, len(writeAuthorityInventory))
	for table, class := range writeAuthorityInventory {
		out[table] = class
	}
	return out
}
