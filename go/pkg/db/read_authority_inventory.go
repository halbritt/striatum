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
	// ReadClassRuntimeColumnScoped: the runtime role holds direct SELECT only on
	// named non-secret columns; sensitive columns are denied and must be read
	// through daemon-authorized projections.
	ReadClassRuntimeColumnScoped ReadAuthorityClass = "runtime_column_scoped_select"
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
	"schema_authority":  ReadClassRuntimeParity,
	"owner_bundle_meta": ReadClassRuntimeParity,

	// Sensitive broad SELECT surfaces. These are the tables a future #164 split
	// should consider first for denial, projection, or row/column scoping.
	"artifacts":                      ReadClassRuntimeSensitive,
	"blockers":                       ReadClassRuntimeSensitive,
	"client_capabilities":            ReadClassRuntimeSensitive,
	"command_requests":               ReadClassRuntimeSensitive,
	"conversations":                  ReadClassRuntimeSensitive,
	"conversation_post_dialog_hooks": ReadClassRuntimeSensitive,
	"cross_repo_run_repositories":    ReadClassRuntimeSensitive,
	"cross_repo_runs":                ReadClassRuntimeSensitive,
	"daemon_supervisors":             ReadClassRuntimeSensitive,
	"escalation_inbox":               ReadClassRuntimeSensitive,
	"events":                         ReadClassRuntimeSensitive,
	// generated_records (RFC 0171 / D273): daemon-indexed blob pointers for
	// generated operator/run-shaped bodies. It stores source paths, blob keys,
	// run/job/artifact linkage, and retention metadata, so it stays in the
	// sensitive broad-SELECT cohort while #164 remains open.
	"generated_records": ReadClassRuntimeSensitive,
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
	"dissent_ledger":     ReadClassRuntimeSensitive,
	"interrogations":     ReadClassRuntimeSensitive,
	"job_dependencies":   ReadClassRuntimeSensitive,
	"job_recovery_state": ReadClassRuntimeSensitive,
	"job_workspaces":     ReadClassRuntimeSensitive,
	"job_worktrees":      ReadClassRuntimeSensitive,
	// lane_uid_leases (RFC 0168 / D272): host uid lifecycle and scrub proof
	// state. It is selected by the runtime daemon for launch, attestation,
	// recovery, and doctor surfaces.
	"lane_uid_leases": ReadClassRuntimeSensitive,
	"jobs":            ReadClassRuntimeSensitive,
	"leases":          ReadClassRuntimeSensitive,
	// RFC 0167 P0 / owner bundle 0022: operator identity surfaces. Both keep a
	// COLUMN gate (the principal_clients precedent) — principal_id (and client_id
	// on operator_sessions) is denied so a leaked runtime credential cannot
	// reconstruct client->principal; the remaining lease/lifecycle columns stay
	// selectable for the lease walk / heartbeat / close. Identity reads ride the
	// run_origin_identity / runs_for_origin_client DEFINER projections.
	"operator_handles":            ReadClassRuntimeSensitive,
	"operator_sessions":           ReadClassRuntimeSensitive,
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
	"verifier_attestations":   ReadClassRuntimeSensitive,
	"verdicts":                ReadClassRuntimeSensitive,
	"work_packets":            ReadClassRuntimeSensitive,
	"workflow_accepted_risks": ReadClassRuntimeSensitive,
	"workflow_snapshots":      ReadClassRuntimeSensitive,

	// Column-scoped runtime reads. Bundle 0005 formalizes the clients token
	// secret gate: token_hash/token_salt are denied, named non-secret metadata
	// stays directly selectable, and secret reads go through daemon-authorized
	// SECURITY DEFINER projections.
	"clients": ReadClassRuntimeColumnScoped,

	// Operational metadata and chain pointers. Still selected by the runtime
	// role in the current broad posture; not a private-read-denial claim.
	"apply_receipts":                 ReadClassRuntimeOperational,
	"audit_chain_head":               ReadClassRuntimeOperational,
	"audit_log":                      ReadClassRuntimeOperational,
	"audit_repositories":             ReadClassRuntimeOperational,
	"audit_segments":                 ReadClassRuntimeOperational,
	"auto_finalize_circuit_breakers": ReadClassRuntimeOperational,
	// cullable_entity (RFC 0170 P0 / D271): observe-only candidacy bookkeeping
	// read by the daemon; it carries no user/agent prose and drives no P0 action.
	"cullable_entity":           ReadClassRuntimeOperational,
	"cross_repo_cycle_counters": ReadClassRuntimeOperational,
	"daemon_meta":               ReadClassRuntimeOperational,
	// deploy_cursor / deploy_plan / deploy_receipt (RFC 0142 P4, migration 0044):
	// the runtime-owned deploy-coordinator substrate. The serving daemon SELECTs
	// deploy_cursor + deploy_plan on every decoupled boot (CheckDeployActivation),
	// and the single-role deployer reads the receipt trail — operational metadata,
	// like schema_state / schema_migrations.
	"deploy_cursor":  ReadClassRuntimeOperational,
	"deploy_plan":    ReadClassRuntimeOperational,
	"deploy_receipt": ReadClassRuntimeOperational,
	// event_chain_segments (RFC 0136 P1, migration 0041): the per-repository event
	// chain-segment seal ledger (first/last boundary ids + hashes + cross-segment
	// witnesses + retention_state). Operational chain metadata the runtime role
	// SELECTs to seal and to prove continuity — like audit_segments /
	// repo_event_chain_heads, not a sensitive prose surface.
	"event_chain_segments":   ReadClassRuntimeOperational,
	"repo_event_chain_heads": ReadClassRuntimeOperational,
	"repo_migrations":        ReadClassRuntimeOperational,
	"rpc_methods":            ReadClassRuntimeOperational,
	"schema_meta":            ReadClassRuntimeOperational,
	"schema_migrations":      ReadClassRuntimeOperational,
	// schema_state (RFC 0142 P3, migration 0043): the runtime-owned schema
	// fingerprint singleton the daemon SELECTs on boot (db.LiveFingerprint) to
	// detect migration / owner-bundle drift, and self-records on a successful
	// migrate — operational metadata, like schema_meta / schema_migrations.
	"schema_state": ReadClassRuntimeOperational,
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

// RuntimeColumnScopedReadTables returns the sorted tables whose sensitive
// columns are denied while named non-secret columns remain directly selectable.
func RuntimeColumnScopedReadTables() []string {
	var out []string
	for table, class := range readAuthorityInventory {
		if class == ReadClassRuntimeColumnScoped {
			out = append(out, table)
		}
	}
	sort.Strings(out)
	return out
}

// RuntimeDeniedReadColumns returns the narrow column-level denials that have
// landed ahead of the full #164 table/projection split. The clients table is
// now classified as runtime_column_scoped_select; other column gates remain in
// their current table classes until a focused slice reclassifies them.
func RuntimeDeniedReadColumns() map[string][]string {
	return map[string][]string{
		"clients":           {"token_hash", "token_salt"},
		"principal_clients": {"principal_id"},
		// RFC 0167 P0 / owner bundle 0022 column gates (the composed-route closure):
		// runs.created_by_principal_id (C2" Route 2), operator_handles.principal_id
		// (C2" Route 1), operator_sessions.{principal_id,client_id} (C2').
		"runs":              {"created_by_principal_id"},
		"operator_handles":  {"principal_id"},
		"operator_sessions": {"principal_id", "client_id"},
	}
}
