package rpc

// ErrorCatalogEntry describes one stable error code the daemon (or its CLI
// client shim) can return: the code itself, a one-line meaning, and the
// default remediation an agent can act on in-band (RFC 0111 P3). Suggestion
// may be empty where no generic remediation is sensible.
type ErrorCatalogEntry struct {
	Code       string
	Meaning    string
	Suggestion string
}

// ErrorCatalog is the closed contract enumerating every error code in live
// use. error_catalog_test.go guard-reconciles it in both directions against
// the Go source under go/: an emitted code missing here fails the guard, and
// an entry here that no source literal emits fails the guard. The catalog is
// also rendered as the "Error code catalog" section of
// docs/reference/command-authority-matrix.md (doc drift is guard-tested too).
//
// ErrorResponse uses DefaultSuggestion to centrally fill rpc.Error.Suggestion
// when a call site did not set one (RFC 0111 P2): explicit call-site
// suggestions always win over these defaults.
var ErrorCatalog = []ErrorCatalogEntry{
	{
		Code:       "artifact_error",
		Meaning:    "An artifact operation (publish, read, validation, or flag contract) failed.",
		Suggestion: "Read the message for the failing artifact constraint, fix the artifact (front matter, paths, required flags), and retry the publish.",
	},
	{
		Code:       "audit_append_failed",
		Meaning:    "The daemon could not append the audit row, so the operation was refused or rolled back (fail-closed provenance).",
		Suggestion: "Retry once; if it persists, check daemon PostgreSQL health with `striatum doctor` and report the audit_id.",
	},
	{
		Code:       "autonomous_worktree_isolation_required",
		Meaning:    "A supervised or agent-loop repo-write lane is configured to use the shared checkout without a recorded interactive-human compatibility override.",
		Suggestion: "Set worktree_isolation: per_job on the repo-write lane, or set allow_shared_checkout_repo_write=true with a non-empty shared_checkout_repo_write_rationale for an explicit interactive-human compatibility workflow.",
	},
	{
		Code:       "bad_host",
		Meaning:    "The MCP endpoint rejected a request whose Host header is not loopback.",
		Suggestion: "Call the daemon MCP endpoint via its loopback address exactly as provided in STRIATUM_MCP_URL.",
	},
	{
		Code:       "bad_origin",
		Meaning:    "The MCP endpoint rejected a browser-style request whose Origin header is not loopback.",
		Suggestion: "Send requests from a loopback origin or drop the Origin header.",
	},
	{
		Code:       "base_head_mismatch",
		Meaning:    "The current git HEAD does not match the commit_request base_head.",
		Suggestion: "Regenerate the commit_request against the current HEAD, then re-run git.commit_apply.",
	},
	{
		Code:       "barrier_blocked",
		Meaning:    "A sealed expectation barrier (RFC 0135) cannot fire because a live in-edge is a blocking contribution or an unresolvable seat (BARRIER_BLOCKED), not a clean terminal gap.",
		Suggestion: "Resolve the blocked seat(s) listed in blocked_manifest (clear the blocker, complete or recover the seat), or recover the run; then re-run `striatum join verify <barrier-id>`.",
	},
	{
		Code:       "barrier_integrity_failed",
		Meaning:    "A sealed expectation barrier (RFC 0135) failed integrity verification: its manifest does not match the staged refs at the live seal, or its assembly journal is inconsistent (unreachable target, committed-manifest mismatch, or terminal failure).",
		Suggestion: "Inspect the problems and manifest in the verify result; recover the assembly through the daemon (do not hand-finish), then re-run `striatum join verify <barrier-id>`.",
	},
	{
		Code:       "barrier_smuggled_content",
		Meaning:    "A fan-in staging contribution smuggles content into the join (RFC 0133 Risks / #352, #353): a merge commit in its chain grafts an off-base side branch, the frozen tip's tree no longer matches the sealed frozen_tip_tree_sha, or the contribution does not descend from the frozen base AND is a contaminated base rather than a recoverable drift (disjoint history, or an off-base foreign root the frozen base does not share). A legitimate base drift (the run branch evolved under the sibling's feet, sharing a real merge-base and no foreign root) is recovered as an extra-parent leg, not refused; this code marks the cases that cannot be recovered.",
		Suggestion: "Re-author the contribution on top of the frozen base or its evolved lineage (no merge of an off-base / disjoint branch) and re-stage; if the frozen tip's tree was re-pointed, the run is exposing a corrupted base — recover it through the daemon, do not hand-finish.",
	},
	{
		Code:       "blob_apply_required",
		Meaning:    "The blob bucket does not exist and creation was not authorized.",
		Suggestion: "Re-run `striatum repo add <path> --apply-blob-creation` to create the bucket.",
	},
	{
		Code:       "blob_disabled",
		Meaning:    "The daemon is not configured for blob storage.",
		Suggestion: "",
	},
	{
		Code:       "blob_head_failed",
		Meaning:    "The blob backend failed to stat an object.",
		Suggestion: "",
	},
	{
		Code:       "blob_list_failed",
		Meaning:    "The blob backend failed to list a bucket.",
		Suggestion: "",
	},
	{
		Code:       "blob_provision_failed",
		Meaning:    "The blob backend failed to provision the repository bucket.",
		Suggestion: "",
	},
	{
		Code:       "blob_publish_failed",
		Meaning:    "Uploading an artifact body to the blob backend failed (including post-upload sha256 mismatch).",
		Suggestion: "",
	},
	{
		Code:       "blob_read_failed",
		Meaning:    "Reading an object from the blob backend failed.",
		Suggestion: "",
	},
	{
		Code:       "branch_confirmation_required",
		Meaning:    "The run branch is not confirmed (or the run is not started), so claims are refused.",
		Suggestion: "Confirm the run branch and start the run (`striatum run start --run-id <id> --branch <name>`) before claiming work.",
	},
	{
		Code:       "branch_mismatch",
		Meaning:    "The current git branch does not match the commit_request branch.",
		Suggestion: "Check out the branch named in the commit_request, then retry.",
	},
	{
		Code:       "capability_denied",
		Meaning:    "The token is valid but this session may not perform the requested action (for example: not the floor holder, interrogator, or target session).",
		Suggestion: "Verify you are the session the action belongs to and re-issue the call from that session; do not act for other lanes.",
	},
	{
		Code:       "capability_expired",
		Meaning:    "The granted capability has expired.",
		Suggestion: "Re-register the session (session.register) or ask the operator to mint a fresh capability token, then retry.",
	},
	{
		Code:       "capability_missing",
		Meaning:    "The token does not carry the capability the method requires.",
		Suggestion: "Use a token that grants the required capability the error names: re-register the session, or have an admin mint one with `striatum daemon token-create --capability <name>` (see docs/how-to/how-to-human.md).",
	},
	{
		Code:       "capability_scope_mismatch",
		Meaning:    "The capability is scoped to a different repository than the request targets.",
		Suggestion: "Re-issue the call with the repository_id the token is scoped to, or obtain a token scoped to this repository.",
	},
	{
		Code:       "commit_request_not_found",
		Meaning:    "The referenced commit_request artifact does not exist or is not readable.",
		Suggestion: "Publish the commit_request artifact first, then retry with its request_id.",
	},
	{
		Code:       "concurrent_run_isolation_required",
		Meaning:    "Another run is already active on the repository and this run has a repo-write job on a lane without worktree_isolation: per_job, so starting it would share the main checkout (RFC 0108 Phase 2).",
		Suggestion: "Set worktree_isolation: per_job on the run's repo-write lane so each run gets its own detached worktree, then start the run; or wait for the active run to finish.",
	},
	{
		Code:       "confirmation_required",
		Meaning:    "A mutating verb needs an explicit confirmation that was not supplied or did not match.",
		Suggestion: "Re-run with the explicit confirmation the message names (for example confirm=true with a matching confirm_request_id).",
	},
	{
		Code:       "conflict",
		Meaning:    "A uniqueness or attribution conflict (for example a client already attributed to another principal).",
		Suggestion: "",
	},
	{
		Code:       "cross_run_collision",
		Meaning:    "Starting this run collides with another active run on the repository — they target the same git branch (RFC 0108 Phase 3).",
		Suggestion: "Give this run a distinct branch (each parallel run integrates on its own branch), or pass --allow-overlap to start anyway and resolve the overlap at integration.",
	},
	{
		Code:       "daemon_auth_lost",
		Meaning:    "The daemon's authority secret no longer matches the database registry (the row is missing or was superseded by a concurrent rotator), so an authorized write was refused (RFC 0110 §4.5).",
		Suggestion: "Restart the daemon to re-bootstrap its authority, or check for a concurrent rotator on the same runtime role (use per-instance roles for a shared PostgreSQL).",
	},
	{
		Code:       "daemon_db_missing",
		Meaning:    "The operation requires daemon PostgreSQL, which is not configured or reachable.",
		Suggestion: "Check daemon PostgreSQL health with `striatum doctor` and restore the database before retrying.",
	},
	{
		Code:       "daemon_under_load",
		Meaning:    "The operation timed out behind transient daemon back-pressure (a statement_timeout/57014 or lock_timeout/55P03 event-append/lifecycle convoy under multi-run supervise load) rather than a real refusal, after the daemon already retried it (#198/#355/#389/#383).",
		Suggestion: "Retry the operation shortly; if it persists, check daemon PostgreSQL load with `striatum doctor` and look for a long-held lock on repo_event_chain_heads.",
	},
	{
		Code:       "daemon_unreachable",
		Meaning:    "The CLI could not reach the daemon RPC socket.",
		Suggestion: "Ensure striatumd is running (`striatum doctor` reports daemon health), then retry.",
	},
	{
		Code:       "dirty_tree_outside_commit_request",
		Meaning:    "The working tree has changes outside the commit_request included_paths.",
		Suggestion: "Commit or revert the changes outside included_paths, then retry.",
	},
	{
		Code:       "displaced_session_live",
		Meaning:    "session.register --replace would displace a session that has heartbeated within the lease's heartbeat window, so it is still live and may be actively driving the same work packet (#189).",
		Suggestion: "Confirm the displaced session is genuinely wedged (check `striatum list sessions` and recent heartbeats); if so, retry with --force-live --reason \"...\" to record why the live lane is being superseded, or close it first with `striatum session close <id>`.",
	},
	{
		Code:       "duplicate_request",
		Meaning:    "The RPC request_id was already used.",
		Suggestion: "Re-issue the call with a fresh request_id.",
	},
	{
		Code:       "duplicate_original_manifest_path",
		Meaning:    "A records migration verification manifest contains the same source path more than once.",
		Suggestion: "Regenerate the inventory manifest and retry verification; do not delete source files until the manifest is unique and byte-identical reconstruction passes.",
	},
	{
		Code:       "duplicate_reconstructed_manifest_path",
		Meaning:    "Records migration reconstruction produced the same source path more than once.",
		Suggestion: "Inspect the generated_records rows for duplicate source path/import metadata, fix the index through daemon-backed migration tooling, then rerun verification.",
	},
	{
		Code:       "event_payload_rejected",
		Meaning:    "A durable event payload was refused by the database write boundary: it carried a transcript key (stdout/stderr/transcript/raw_output/provider_output) or exceeded the durable-event size cap (RFC 0110 §12, C-EVENT-NO-TRANSCRIPTS).",
		Suggestion: "Record curated coordination state in the event, not captured agent output; transcripts belong in operator-local diagnostics, not the durable event chain.",
	},
	{
		Code:       "file_read_failed",
		Meaning:    "The daemon could not read a repository file it was asked to operate on.",
		Suggestion: "Verify the path exists and is readable inside the repository, then retry.",
	},
	{
		Code:       "fresh_review_byte_identical",
		Meaning:    "A review.submit finding is byte-identical (content_sha256) to this job's prior-attempt finding, so it re-asserts a stale verdict against a revised target instead of a fresh review (#206).",
		Suggestion: "Delete the stale finding file left at the artifact path by the prior round, read the CURRENT revision of the target, and write your own finding before resubmitting.",
	},
	{
		Code:       "git_commit_apply_failed",
		Meaning:    "A git step of commit apply failed.",
		Suggestion: "",
	},
	{
		Code:       "git_snapshot_failed",
		Meaning:    "Capturing the git snapshot failed.",
		Suggestion: "",
	},
	{
		Code:       "git_unavailable",
		Meaning:    "The git executable is not available to the daemon.",
		Suggestion: "Install git and ensure it is on the daemon's PATH.",
	},
	{
		Code:       "internal_error",
		Meaning:    "An unexpected daemon-side failure that does not map to a stable code.",
		Suggestion: "Retry once; if it persists, report the failure with its audit_id.",
	},
	{
		Code:       "invalid_transition",
		Meaning:    "The requested state transition is not legal from the current job, run, lease, or session state.",
		Suggestion: "Re-read the live state (job.detail / run.detail, or `striatum status`) and take only the transition the current state allows.",
	},
	{
		Code:       "key_rotation_unavailable",
		Meaning:    "daemon.key.rotate is not wired in this daemon build; signing keys were not modified.",
		Suggestion: "",
	},
	{
		Code:       "lease_error",
		Meaning:    "The supplied lease is missing, expired, inactive, owned by another session, or bound to a different job.",
		Suggestion: "Heartbeat your lease (work.heartbeat); if it is stale, recover stale leases (`striatum recovery stale-leases`) and re-claim via work.await_packet.",
	},
	{
		Code:       "lane_credential_cache_inside_repo",
		Meaning:    "A provider-owned credential/cache selector resolves inside the target repository, where a repo-write lane could read or mutate it.",
		Suggestion: "Move the provider credential/cache directory outside the target repository, then retry supervise.start.",
	},
	{
		Code:       "lane_provider_auth_failed",
		Meaning:    "The lane provider-auth preflight found missing, stale, expired, revoked, or unrefreshable provider credentials for the lane identity.",
		Suggestion: "Refresh the provider login for the lane OS user, then retry supervise.start.",
	},
	{
		Code:       "lane_provider_binary_missing",
		Meaning:    "The lane provider-auth preflight could not find or execute the provider CLI under the lane launch environment.",
		Suggestion: "Install the provider CLI for the lane launch environment or add the binary directory to lane path_prefix.",
	},
	{
		Code:       "lane_provider_preflight_launch_failed",
		Meaning:    "Striatum could not start the closed provider-auth smoke command under the intended lane identity.",
		Suggestion: "Fix lane run-as user, sudo, home directory, and launch environment provisioning.",
	},
	{
		Code:       "lane_provider_preflight_timeout",
		Meaning:    "The lane provider-auth preflight timed out, including hung refresh paths or interactive prompts.",
		Suggestion: "Inspect the lane provider login for an interactive prompt or hung refresh path.",
	},
	{
		Code:       "lane_provider_preflight_unexpected_result",
		Meaning:    "The lane provider-auth smoke reached an unsupported result shape that cannot be classified as auth success, auth failure, launch failure, timeout, binary missing, or provider unavailable.",
		Suggestion: "Inspect the provider CLI manually; the smoke completed with an unsupported result shape.",
	},
	{
		Code:       "lane_provider_preflight_unsupported",
		Meaning:    "The selected provider-auth gate mode requires a provider or lane shape that has no supported smoke.",
		Suggestion: "Use --provider-auth-gate off or configure a provider with a supported auth preflight.",
	},
	{
		Code:       "lane_provider_unavailable",
		Meaning:    "Network, provider service, rate limit, or provider-side availability prevented the lane provider-auth preflight from reaching an auth conclusion.",
		Suggestion: "Retry after provider or network availability recovers.",
	},
	{
		Code:       "lane_uid_generation_mismatch",
		Meaning:    "A supervised lane's uid lease generation no longer matches the active daemon lease row, so attestation/control/reporting was refused fail-closed.",
		Suggestion: "Stop and relaunch the supervised lane so it receives a fresh uid lease generation; inspect `striatum doctor` if the old lease is quarantined.",
	},
	{
		Code:       "lane_uid_generation_missing",
		Meaning:    "A supervised lane references a uid lease without carrying the expected lease generation metadata.",
		Suggestion: "Stop and relaunch the supervised lane with the current daemon binary; inspect supervisor metadata if this persists.",
	},
	{
		Code:       "lane_uid_lease_missing",
		Meaning:    "A supervised lane references a uid lease row that is absent from the daemon ledger.",
		Suggestion: "Stop and relaunch the supervised lane; run `striatum doctor` to check lane_uid_leases integrity.",
	},
	{
		Code:       "lane_uid_pool_exhausted",
		Meaning:    "Every configured lane uid pool entry is active, scrubbing, or quarantined, so supervise.start cannot allocate an isolated OS uid.",
		Suggestion: "Wait for active lanes to finish, recover/quarantine stuck uid leases, or add more users to STRIATUM_LANE_UID_POOL.",
	},
	{
		Code:       "lane_uncovered_credential_selector_inside_repo",
		Meaning:    "A provider-owned credential/cache selector not modeled by Striatum resolves inside the target repository, so the provider auth boundary cannot be proven.",
		Suggestion: "Move that provider selector outside the repository or add an explicit resolver model before retrying supervise.start.",
	},
	{
		Code:       "merge_conflict",
		Meaning:    "Integrating a run's branch into the target mainline conflicts; the merge was refused and mainline left untouched (RFC 0108 Phase 4 never auto-resolves).",
		Suggestion: "Rebase or resolve the run branch against the target on a branch a maintainer merges, then re-run run.integrate; the conflicting paths are in the error details.",
	},
	{
		Code:       "missing_reconstructed_record",
		Meaning:    "Records migration verification could not reconstruct a manifest entry from generated_records and blob storage.",
		Suggestion: "Run `striatum records migration import --manifest <manifest>` for the safe entries, then rerun verification; do not delete the tracked source file.",
	},
	{
		Code:       "worktree_head_unreachable",
		Meaning:    "worktree.release refused because the worktree HEAD is not reachable from the run branch or a refs/striatum pin.",
		Suggestion: "Complete the job so work.complete anchors the commits, or rerun worktree release with --force only if discarding that HEAD is intentional.",
	},
	{
		Code:       "worktree_required",
		Meaning:    "A repo-write job on a lane with worktree_isolation: per_job tried to publish, write, run, or complete without an active job worktree.",
		Suggestion: "Run worktree.create using the active session, job, and lease from the work packet, then retry the operation.",
	},
	{
		Code:       "method_unknown",
		Meaning:    "The method has no registered handler.",
		Suggestion: "Call tools/list and use a method the daemon actually exposes.",
	},
	{
		Code:       "not_found",
		Meaning:    "A referenced entity (session, job, artifact, interrogation, ...) does not exist.",
		Suggestion: "List the live entities first (list.runs / list.jobs / list.sessions / artifact.list_for_run) and re-issue with an id that exists.",
	},
	{
		Code:       "not_implemented",
		Meaning:    "The method is registered but not implemented in this daemon build.",
		Suggestion: "",
	},
	{
		Code:       "path_conflict",
		Meaning:    "The active repository path is occupied by a different repository identity.",
		Suggestion: "",
	},
	{
		Code:       "path_outside_scope",
		Meaning:    "The path escapes the allowed scope (write scope, export directory, or repository root).",
		Suggestion: "Use a path inside your packet's write_scope.allowed_paths (or the allowed output directory) and retry.",
	},
	{
		Code:       "path_traversal",
		Meaning:    "Path traversal outside the repository was refused.",
		Suggestion: "Use a repository-relative path without `..` segments.",
	},
	{
		Code:       "receipt_missing",
		Meaning:    "The apply receipt was not found.",
		Suggestion: "",
	},
	{
		Code:       "repo_blob_conflict",
		Meaning:    "The repository's blob bucket is owned by a different repository identity.",
		Suggestion: "",
	},
	{
		Code:       "repo_not_found",
		Meaning:    "The repository path does not exist on disk.",
		Suggestion: "Verify the repository path and re-run `striatum repo add` with the correct location.",
	},
	{
		Code:       "repo_not_registered",
		Meaning:    "The repository is not registered with the daemon.",
		Suggestion: "Register the repository first (`striatum repo add`), then retry.",
	},
	{
		Code:       "repo_scratch_missing",
		Meaning:    "The repository scratch area is not initialized.",
		Suggestion: "Run `striatum repo add <path> --init` for the target repository, then retry.",
	},
	{
		Code:       "reconstructed_sha256_mismatch",
		Meaning:    "Records migration reconstruction produced bytes whose sha256 differs from the original manifest.",
		Suggestion: "Inspect the generated_records row and blob object; re-import through daemon RPC before considering deletion.",
	},
	{
		Code:       "reconstructed_size_mismatch",
		Meaning:    "Records migration reconstruction produced bytes whose size differs from the original manifest.",
		Suggestion: "Inspect the generated_records row and blob object; re-import through daemon RPC before considering deletion.",
	},
	{
		Code:       "review_provenance_override_required",
		Meaning:    "An unattested/operator-authored accepting review verdict requires an explicit run-level review provenance decision.",
		Suggestion: "Record an accepting decision with `--escape-surface review_provenance --escape-action <action> --rationale <reason>`, then retry with `--review-provenance-decision-id <decision_id>`.",
	},
	{
		Code:       "run_not_found",
		Meaning:    "The run_id was not found.",
		Suggestion: "List runs (list.runs) and use an existing run_id.",
	},
	{
		Code:       "schema_invalid",
		Meaning:    "The request failed schema validation (missing, ill-typed, or malformed parameters or envelope).",
		Suggestion: "Fix the named parameter to match the documented schema and resend the request.",
	},
	{
		Code:       "session_token_stale",
		Meaning:    "A session-bound token belongs to a closed predecessor session but was used to act as that lane's active successor session.",
		Suggestion: "Stop the stale lane and run supervise.start for the active successor session so it receives its own session-bound token; use supervise.rebridge only to repair that successor's existing supervisor, then retry from the successor lane.",
	},
	{
		Code:       "session_inactive",
		Meaning:    "A terminal or inactive session tried to publish or complete work after losing authority for the lane.",
		Suggestion: "Recover through daemon state: requeue the job on the same attempt (`striatum recovery requeue-stale --run-id <run_id> --job-id <job_id> --force --justification \"...\"`), then register or supervise.start a fresh session and retry from that fresh session. If the session is still active but about to be closed, use `striatum session close --session-id <session_id> --reason \"...\" --requeue-job`.",
	},
	{
		Code:       "sha256_mismatch",
		Meaning:    "A file body sha256 does not match the published artifact's content_sha256 (the repo file drifted).",
		Suggestion: "Re-publish the artifact from the current file (artifact.publish) or restore the file to the published content before retrying.",
	},
	{
		Code:       "shutdown_unavailable",
		Meaning:    "daemon.shutdown is not wired in this daemon process.",
		Suggestion: "Stop the daemon with its service manager or a signal instead.",
	},
	{
		Code:       "signing_key_insecure",
		Meaning:    "The sealed-apply signing key fails security requirements (for example permissions).",
		Suggestion: "",
	},
	{
		Code:       "signing_key_invalid",
		Meaning:    "The sealed-apply signing key is invalid or unusable.",
		Suggestion: "",
	},
	{
		Code:       "spawn_grant_expired",
		Meaning:    "The run's spawn-authorization grant has expired, so the daemon auto_spawn scheduler refuses to spawn under a stale grant (RFC 0122 C2).",
		Suggestion: "Re-authorize the run by restarting it (run.start re-captures a fresh grant), or drive the run manually.",
	},
	{
		Code:       "spawn_grant_missing",
		Meaning:    "An auto_spawn run has queued lane work but no active spawn-authorization grant; the daemon scheduler cannot invent authority (RFC 0122 C2).",
		Suggestion: "Re-run run.start to capture a grant, or drive the run manually with `run drive`.",
	},
	{
		Code:       "spawn_grant_no_owner_principal",
		Meaning:    "An auto_spawn run.start had no authenticated owner principal to capture, so there is no identity for the scheduler to replay.",
		Suggestion: "Authenticate run.start with a capability token that carries a principal, then retry.",
	},
	{
		Code:       "spawn_run_as_unresolved",
		Meaning:    "An auto_spawn run's run-as identity (the configured lane OS user) cannot be resolved on this host, so the scheduler would spawn into a non-existent identity (RFC 0122 §4).",
		Suggestion: "Provision the lane OS user (with a home directory) or unset STRIATUM_LANE_OS_USER to run lanes as the daemon user, then restart the run.",
	},
	{
		Code:       "unexpected_reconstructed_record",
		Meaning:    "Records migration reconstruction produced a path that was not present in the original manifest.",
		Suggestion: "Inspect the import batch selector and generated_records rows; verify only the intended manifest before materializing or deleting anything.",
	},
	{
		Code:       "stale_daemon_identity",
		Meaning:    "The MCP request presented a boot epoch that does not match the live daemon's, so it dialed a recycled port now bound by a different daemon process run and was refused before touching run state (#316).",
		Suggestion: "Relaunch the lane against the current daemon (re-run supervise.start, or recover the stalled lane) so it carries the live daemon's boot epoch; do not reuse a stale on-disk MCP endpoint/config pin.",
	},
	{
		Code:       "symlink_refused",
		Meaning:    "A symlinked path was refused (repository registration and scoped writes resolve real paths).",
		Suggestion: "Use the real (non-symlinked) path and retry.",
	},
	{
		Code:       "target_unavailable",
		Meaning:    "The target session for the requested operation does not exist or is unavailable.",
		Suggestion: "List live sessions (list.sessions) and target one that is active.",
	},
	{
		Code:       "token_expired",
		Meaning:    "The capability token is expired.",
		Suggestion: "Obtain a fresh capability token (re-register the session or ask the operator), then resend with the new token.",
	},
	{
		Code:       "token_invalid",
		Meaning:    "The capability token does not exist or its secret does not match.",
		Suggestion: "Resend with a valid capability token exactly as issued; if yours was rotated, obtain a fresh one from the operator or session registration.",
	},
	{
		Code:       "token_malformed",
		Meaning:    "The capability token is not in the issued token_id.secret form.",
		Suggestion: "Send the capability token exactly as issued (token_id.secret) in the Authorization bearer header.",
	},
	{
		Code:       "token_missing",
		Meaning:    "No capability token was supplied on a method that requires one.",
		Suggestion: "Send your capability token as an Authorization bearer header (lane environments provide STRIATUM_MCP_TOKEN).",
	},
	{
		Code:       "token_revoked",
		Meaning:    "The capability token has been revoked.",
		Suggestion: "Obtain a fresh capability token (re-register the session or ask the operator), then resend with the new token.",
	},
	{
		Code:       "token_scope_ambiguous",
		Meaning:    "The token carries duplicate active capability scopes, so the daemon cannot pick one.",
		Suggestion: "",
	},
	{
		Code:       "token_unavailable",
		Meaning:    "The CLI could not load a capability token from its configured token file.",
		Suggestion: "Run inside a workflow lane (which provides the token) or point the CLI at a readable capability-token file.",
	},
	{
		Code:       "version_incompatible",
		Meaning:    "The client and daemon share no supported envelope version.",
		Suggestion: "Upgrade so client and daemon match (`make install`, then restart striatumd so the running image is the new build).",
	},
	{
		Code:       "write_scope_drift",
		Meaning:    "A job attempted to publish or complete work outside the frozen write scope for its current attempt.",
		Suggestion: "Use the path in the active work packet, clear or move out-of-scope changes, or route through audited recovery (`striatum recovery resume` for remediated write-scope blockers, or a fresh/replacement attempt for legitimate scope changes).",
	},
	{
		Code:       "workflow_error",
		Meaning:    "Workflow validation, preparation, or run orchestration failed.",
		Suggestion: "",
	},
	{
		Code:       "workflow_snapshot_not_found",
		Meaning:    "The workflow_snapshot_id was not found.",
		Suggestion: "Read the run's workflow_snapshot_id from run.detail and re-issue with it.",
	},
}

var errorCatalogByCode = func() map[string]ErrorCatalogEntry {
	byCode := make(map[string]ErrorCatalogEntry, len(ErrorCatalog))
	for _, entry := range ErrorCatalog {
		byCode[entry.Code] = entry
	}
	return byCode
}()

// LookupErrorCode returns the catalog entry for code.
func LookupErrorCode(code string) (ErrorCatalogEntry, bool) {
	entry, ok := errorCatalogByCode[code]
	return entry, ok
}

// DefaultSuggestion returns the catalog's default remediation for code, or ""
// when the code is unknown or carries no default.
func DefaultSuggestion(code string) string {
	return errorCatalogByCode[code].Suggestion
}
