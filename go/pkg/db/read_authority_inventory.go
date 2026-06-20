package db

import "sort"

// ReadAuthorityClass classifies the current runtime-role read surface for a
// striatumd.* table. This is the #164 companion to the RFC 0110 write-authority
// inventory: it does not claim private-read denial, but it keeps the broad
// SELECT posture explicit and table-scoped until a successor narrows it.
type ReadAuthorityClass string

const (
	// ReadClassRuntimeSensitive: the runtime role currently holds SELECT and the
	// table can expose user/agent prose, repository paths, token-adjacent data,
	// principal/session identity, or other private workflow metadata if a live
	// runtime credential leaks.
	ReadClassRuntimeSensitive ReadAuthorityClass = "runtime_sensitive_select"
	// ReadClassRuntimeOperational: the runtime role currently holds SELECT for
	// daemon operation, but the table is not one of the representative sensitive
	// surfaces used in the #164 doctor posture.
	ReadClassRuntimeOperational ReadAuthorityClass = "runtime_operational_select"
	// ReadClassRuntimeParity: an owner-maintained table deliberately readable by
	// the runtime role for startup capability parity.
	ReadClassRuntimeParity ReadAuthorityClass = "runtime_parity_select"
	// ReadClassRuntimeDenied: the runtime role must not hold SELECT.
	ReadClassRuntimeDenied ReadAuthorityClass = "runtime_select_denied"
	// ReadClassRuntimeProjection: the table is readable by the daemon only
	// through daemon-authorized SECURITY DEFINER projections; the runtime role
	// holds no table SELECT (RFC 0114, owner bundle 0006).
	ReadClassRuntimeProjection ReadAuthorityClass = "runtime_projection_read"
)

var readAuthorityInventory = map[string]ReadAuthorityClass{
	// Owner-only authority surfaces.
	"daemon_auth_registry": ReadClassRuntimeDenied,
	"daemon_auth_log":      ReadClassRuntimeDenied,
	"owner_bundle_meta":    ReadClassRuntimeDenied,

	// RFC 0114 / owner bundle 0006: session↔credential linkage, denied with no
	// projection — nothing in go/ consumes it (the table is vestigial since the
	// Python runtime retired; any future session-tracking consumer must read
	// through a new daemon-authorized projection, never via a re-grant).
	"client_sessions": ReadClassRuntimeDenied,

	// RFC 0114 / owner bundle 0006: identity prose readable only through the
	// daemon-authorized projections (get_principal,
	// resolve_principal_for_client, list_principal_scopes).
	"principals": ReadClassRuntimeProjection,

	// Startup parity: owner-maintained, intentionally runtime-readable.
	"schema_authority": ReadClassRuntimeParity,

	// Sensitive broad SELECT surfaces. These are the tables a future #164 split
	// should consider first for denial, projection, or row/column scoping.
	"artifacts":                      ReadClassRuntimeSensitive,
	"blockers":                       ReadClassRuntimeSensitive,
	"client_capabilities":            ReadClassRuntimeSensitive,
	"clients":                        ReadClassRuntimeSensitive,
	"command_requests":               ReadClassRuntimeSensitive,
	"conversations":                  ReadClassRuntimeSensitive,
	"conversation_post_dialog_hooks": ReadClassRuntimeSensitive,
	"cross_repo_run_repositories":    ReadClassRuntimeSensitive,
	"cross_repo_runs":                ReadClassRuntimeSensitive,
	"daemon_supervisors":             ReadClassRuntimeSensitive,
	"escalation_inbox":               ReadClassRuntimeSensitive,
	"events":                         ReadClassRuntimeSensitive,
	// fan-in sealed-barrier tables (RFC 0135 P1, migration 0029): the immutable
	// freeze record and the attempt-addressed staging contributions the live-seal
	// JOIN barrier SELECTs — coordination state, like leases/sessions/jobs.
	"fanin_freeze_points":          ReadClassRuntimeSensitive,
	"barrier_staged_contributions": ReadClassRuntimeSensitive,
	// barrier_state (RFC 0135 P2, migration 0030): the recoverable barrier-assembly
	// journal (sealed -> assembling -> committed|failed) the assembler SELECTs to
	// resume a crash — coordination state, like the staging table above.
	"barrier_state": ReadClassRuntimeSensitive,
	// dissent_ledger (RFC 0135 P4, migration 0032): the forward-written, seal-durable
	// panel-quorum dissent witness the quorum barrier SELECTs to BLOCK finalize
	// wherever recovery moved a seat's lineage — coordination state keyed on the stable
	// workflow_job_id, like the staging table above.
	"dissent_ledger":              ReadClassRuntimeSensitive,
	"interrogations":              ReadClassRuntimeSensitive,
	"job_dependencies":            ReadClassRuntimeSensitive,
	"job_recovery_state":          ReadClassRuntimeSensitive,
	"job_workspaces":              ReadClassRuntimeSensitive,
	"job_worktrees":               ReadClassRuntimeSensitive,
	"jobs":                        ReadClassRuntimeSensitive,
	"leases":                      ReadClassRuntimeSensitive,
	"principal_clients":           ReadClassRuntimeSensitive,
	"process_executions":          ReadClassRuntimeSensitive,
	"process_supervisor_pointers": ReadClassRuntimeSensitive,
	"process_supervisors":         ReadClassRuntimeSensitive,
	"queue_messages":              ReadClassRuntimeSensitive,
	"repositories":                ReadClassRuntimeSensitive,
	"rpc_request_log":             ReadClassRuntimeSensitive,
	"runs":                        ReadClassRuntimeSensitive,
	"scheduler_cursors":           ReadClassRuntimeSensitive,
	"sessions":                    ReadClassRuntimeSensitive,
	// spawn_authorization_grants (RFC 0122): sensitive authorization state
	// (owner principal, run-as, capability envelope) the runtime role SELECTs to
	// drive the auto_spawn scheduler — like leases/sessions/principal_clients.
	"spawn_authorization_grants": ReadClassRuntimeSensitive,
	// supervisor_buffered_packets (#456 / FMA-006): holds buffered work-packet
	// payloads (agent-facing prose) the runtime role SELECTs to replay a
	// no-reader push delivery across a daemon restart — sensitive, like
	// work_packets.
	"supervisor_buffered_packets": ReadClassRuntimeSensitive,
	"trajectory_segments":         ReadClassRuntimeSensitive,
	// verifier_attestations (RFC 0141 / D243 / #482): sensitive trust state the
	// run-completion gate SELECTs to decide whether an external claim may reach
	// VERIFIED — like verdicts/spawn_authorization_grants.
	"verifier_attestations": ReadClassRuntimeSensitive,
	"verdicts":              ReadClassRuntimeSensitive,
	"work_packets":                ReadClassRuntimeSensitive,
	"workflow_accepted_risks":     ReadClassRuntimeSensitive,
	"workflow_snapshots":          ReadClassRuntimeSensitive,

	// Operational metadata and chain pointers. Still selected by the runtime
	// role in the current broad posture; not a private-read-denial claim.
	"apply_receipts":                 ReadClassRuntimeOperational,
	"audit_chain_head":               ReadClassRuntimeOperational,
	"audit_log":                      ReadClassRuntimeOperational,
	"audit_repositories":             ReadClassRuntimeOperational,
	"audit_segments":                 ReadClassRuntimeOperational,
	"auto_finalize_circuit_breakers": ReadClassRuntimeOperational,
	"cross_repo_cycle_counters":      ReadClassRuntimeOperational,
	"daemon_meta":                    ReadClassRuntimeOperational,
	// event_chain_segments (RFC 0136 P1, migration 0041): the per-repository event
	// chain-segment seal ledger (first/last boundary ids + hashes + cross-segment
	// witnesses + retention_state). Operational chain metadata the runtime role
	// SELECTs to seal and to prove continuity — like audit_segments /
	// repo_event_chain_heads, not a sensitive prose surface.
	"event_chain_segments":           ReadClassRuntimeOperational,
	"repo_event_chain_heads":         ReadClassRuntimeOperational,
	"repo_migrations":                ReadClassRuntimeOperational,
	"rpc_methods":                    ReadClassRuntimeOperational,
	"schema_meta":                    ReadClassRuntimeOperational,
	"schema_migrations":              ReadClassRuntimeOperational,
}

// ClassifyReadTable returns the read-authority classification of a striatumd.*
// table and whether it is in the inventory.
func ClassifyReadTable(table string) (ReadAuthorityClass, bool) {
	class, ok := readAuthorityInventory[table]
	return class, ok
}

// ReadAuthorityInventory returns a copy of the full classification.
func ReadAuthorityInventory() map[string]ReadAuthorityClass {
	out := make(map[string]ReadAuthorityClass, len(readAuthorityInventory))
	for table, class := range readAuthorityInventory {
		out[table] = class
	}
	return out
}

// RuntimeSensitiveReadTables returns the sorted tables currently called out as
// sensitive under the broad runtime SELECT posture.
func RuntimeSensitiveReadTables() []string {
	var out []string
	for table, class := range readAuthorityInventory {
		if class == ReadClassRuntimeSensitive {
			out = append(out, table)
		}
	}
	sort.Strings(out)
	return out
}

// RuntimeDeniedReadColumns returns the narrow column-level denials that have
// landed ahead of the full #164 table/projection split. principal_clients
// stays runtime_sensitive_select with a column gate (the clients precedent):
// principal_id — the column that makes the linkage an attribution graph — is
// denied, while client_id/linked_at/unlinked_at remain selectable for the
// live UPDATE ... WHERE in admin/tokens.go (RFC 0114).
func RuntimeDeniedReadColumns() map[string][]string {
	return map[string][]string{
		"clients":           {"token_hash", "token_salt"},
		"principal_clients": {"principal_id"},
	}
}
