# Command Authority Matrix

Status: Go-only authority reference
Date: 2026-06-09
Source inputs: `contracts/daemon_methods.json`,
`go/pkg/cli/routes/routes_generated.go`,
`go/pkg/cli/localcommands/localcommands.go`, `go/pkg/rpc/registry_methods.go`,
and `go/pkg/`.

This matrix names the current authority path for every registered daemon RPC
method plus CLI commands that are intentionally outside the production workflow
mutation path. It is transition scaffolding for the architecture remediation
plan. Contract metadata and CLI route reference tables are generated in
`docs/reference/daemon-method-tables.md`; authority classification,
historical SQLite-dependency notes, and Go authority status still live here.
Every new RPC
method or handwritten route map must update this file. Per D108, executable
guardrails keep this matrix aligned with the daemon contract/runtime while it
remains curated for authority and status classification; the retired Python
`tests/architecture/test_authority_guardrails.py` (RFC 0078) is superseded by
the live Go guards: `go/pkg/rpc/registry_contract_test.go` (registry ↔
`contracts/daemon_methods.json`), `go/pkg/rpc/registry_rfc0043_test.go`
(capability/scope invariants), and `go/pkg/rpc/error_catalog_test.go` (the
error-code catalog below ↔ source literals ↔ this doc). Go daemon handler coverage is also executable in
`go/cmd/striatumd/handler_coverage_test.go`, which fails if active contract
methods are missing Go handlers or regress to generic `not_implemented`
placeholders.

D107 / RFC 0068 supersedes D105. The Go columns below are no longer
D105-bounded reference material; they are the production-port backlog. Any
`placeholder` or SQLite-backed row is active debt before the Python daemon can
retire. D110 removed the SQLite-bound `daemon.migrate_repo_local`,
`dogfood.publish_on_behalf`, and `dogfood.surgical_recovery` RPC names from
the production contract; D112 removed `apply.reviewed_patch` as well. These
names no longer appear as registered methods, and stale calls audit as
`method_unknown`.

The `python authority` column below is retained as retirement provenance for
pre-RFC-0078 rows; it is not a current implementation surface.

Legend:

- **python authority**: `pg` means a native Python Postgres handler is
  registered. `direct` means `DaemonRpcRouter` handles the method without
  `CLI_ROUTES`.
  `local_file_authoring` means the CLI implements a repository-file helper
  directly and daemon RPC fails closed instead of falling back.
- **go authority**: `real` means a production Go handler is registered.
  `placeholder` means the Go fixture returns `not_implemented`.
  Removed unsupported methods are absent from this table and audit as
  `method_unknown`.
- **sqlite dependency** names whether production execution can still open
  repo-local SQLite through dogfood compatibility helpers, local legacy
  service surfaces, or migration-only paths.

## Direct PostgreSQL Bootstrap/Admin Plane

These Go CLI touchpoints may configure or open daemon PostgreSQL directly
because they run before a daemon is healthy or apply owner/admin DDL out of
band. This is not a general live-state mutation escape hatch: ordinary workflow
state changes must route through daemon RPC.

| File/function | Allowed surface | Direct PostgreSQL helper imports | Constraint |
|---|---|---|---|
| `go/pkg/cli/localcommands/daemon.go::runDaemonMigrate` | `daemon migrate-db` | `db.ResolveConfig`, `db.ConnectAndMigrate` | Applies forward PostgreSQL migrations with an owner/admin DSN before the daemon serves; no workflow RPC mutation. |
| `go/pkg/cli/localcommands/daemon.go::runDaemonOwnerDDL` | `daemon owner-ddl apply` | `db.ResolveConfig`, `db.Connect`, `db.ApplyOwnerBundles`, `db.ReassertWriteRevokes`, `db.ReassertReadRevokes` | Applies owner bundles and reasserts protected grants with an owner DSN; no workflow RPC mutation. |
| `go/pkg/cli/localcommands/daemon.go::runDaemonInstall` | `daemon install` | none | Renders the systemd user unit and config scaffold only; refuses (exit 1) when a system unit already owns the daemon (#509). |
| `go/pkg/cli/localcommands/daemon.go::runDaemonStatus` | `daemon status` | none directly | Reports local service/runtime layout and shells to `striatum doctor` for a read-only daemon health check. |

## Registered Daemon Methods

| RPC method | CLI command | Capability | Scope | Python authority | Go authority | CLI fallback | SQLite dependency | Status |
|---|---|---:|---|---|---|---:|---|---|
| `daemon.hello` | n/a | none | daemon_global | direct | real | no | no | stable |
| `daemon.describe` | n/a | read | daemon_global | direct | real | no | no | stable |
| `status` | `status` | read | single_repo | pg | real | no | no | stable |
| `why` | `why` | read | single_repo | pg | real | no | no | stable |
| `doctor` | `doctor` | read | single_repo | pg + git refs | real | no | no | includes read-only worktree ref-safety projection; `--verbose` adds structured records for `worktree_head_unreachable` / `job_completed_without_anchor`; `--verbose` also adds the RFC 0135 P3 `barrier_integrity` block (`barrier_blocked` / `barrier_assembling_target_unreachable` / `barrier_committed_manifest_mismatch` / `barrier_orphaned_staging_ref`); explicit `--lane-provider-auth codex` is the only doctor mode that runs a provider CLI |
| `join.verify` | `join verify` | read | single_repo | n/a (RFC 0135 P3, Go-only) | real | no | no | RFC 0135 P3 (#347) read-only barrier integrity verification over the `barrier_status` view (migration 0031); returns `barrier_integrity_failed` / `barrier_blocked` (with `blocked_manifest`) on a corrupted or blocked barrier so it is usable as a CI/operator gate; no state mutation |
| `doctor.blob_block` | web/MCP blob-block doctor DTO; no CLI route | read | daemon_global | pg + blob store | real | no | no | read-only durable-blob integrity projection over the configured blob client; on-contract per #363 (formerly a runtime `rpc.MethodRegistry` hand-registration the machine contract was blind to) |
| `dashboard` | `dashboard` | read | single_repo | pg | real | no | no | stable |
| `evidence.export` | `evidence export` | read | single_repo | pg | real | no | no | stable |
| `corpus.export` | `corpus export` | read | single_repo | pg | real | no | no | stable |
| `recall.search` | `recall search` | read | single_repo | not implemented in Python RPC | real | no | no | Striatum-native hot-tier read over daemon-owned artifact metadata; no `memory.*` capability, no external memory import, and no state-transition dependency |
| `archive.create` | `archive create` | read | single_repo | pg | real | no | no | Go V1 run archive writer |
| `git.snapshot` | `git snapshot` | read | single_repo | not implemented in Python RPC | real | no | no | Go read-only local Git snapshot; no fetch/push/commit/provider operations |
| `git.commit_apply` | `git commit-apply` | apply | single_repo | not implemented in Python RPC | real | no | no | Go explicit-confirm local commit apply from a confirmed `commit_request` artifact; no push/provider operations |
| `run.summary` | `run summary` | read | single_repo | pg | real | no | no | stable |
| `run.detail` | web run detail DTO | read | single_repo | pg | real | no | no | stable |
| `job.detail` | web job detail DTO | read | single_repo | pg | real | no | no | stable |
| `artifact.get_content` | `artifact get-content`; web artifact content DTO | read | single_repo | not implemented in Python RPC | real | no | no | Go blob/repo-path/git-anchor artifact content read; exposes body_base64 for blob_exhaust findings not surfaced by evidence/corpus export (#506 part 2) |
| `artifact.list_for_run` | `invoke` | read | single_repo | not implemented in Python RPC | real | no | no | Go blob migration verification read |
| `corpus.migrate_historical_dogfood_file` | `corpus migrate-historical-dogfoods` helper | write | single_repo | not implemented in Python RPC | real | no | no | Go historical dogfood blob migration upload |
| `artifact.backfill_blob` | `invoke` | write | single_repo | not implemented in Python RPC | real | no | no | Go artifact blob-reference backfill |
| `corpus.list_historical_dogfoods` | web historical dogfood index DTO | read | single_repo | not implemented in Python RPC | real | no | no | Go historical dogfood blob index |
| `corpus.list_historical_dogfood_files` | web historical dogfood file-list DTO | read | single_repo | not implemented in Python RPC | real | no | no | Go historical dogfood blob file list |
| `corpus.fetch_historical_dogfood_file` | web historical dogfood file DTO | read | single_repo | not implemented in Python RPC | real | no | no | Go historical dogfood blob fetch |
| `run.graph` | `run graph` | read | single_repo | pg | real | no | no | stable |
| `run.events` | web SSE event stream DTO | read | single_repo | pg | real | no | no | stable |
| `run.posture_verdicts` | web posture verdict drill-down | read | single_repo | pg | real | no | no | stable |
| `workflow.validate` | `workflow validate` | read | single_repo | local_file_authoring | real | no | no live state | Go file-authoring validator; no PG state mutation |
| `workflow.lint` | MCP/UI workflow lint; no current Go CLI route | read | single_repo | not implemented in Python RPC | real | no | no live state | Go daemon lint projection with immutable workflow fingerprint and accepted-risk annotations |
| `workflow.plan` | MCP/UI workflow plan; no current Go CLI route | read | single_repo | local_file_authoring | real | no | no live state | Go file-authoring plan projection |
| `workflow.graph` | MCP/UI workflow graph; no current Go CLI route | read | single_repo | local_file_authoring | real | no | no live state | Go file-authoring JSON/Mermaid/DOT projection |
| `workflow.accepted_risks.list` | `workflow accepted-risks` / MCP/UI accepted-risk list | read | single_repo | not implemented in Python RPC | real | no | no live state mutation | Go read projection over daemon-owned workflow accepted-risk records |
| `workflow.templates.list` | `workflow templates list` | read | single_repo | local_file_authoring | real | no | no live state | Go embedded catalog read; CLI remains local authoring surface |
| `workflow.templates.show` | `workflow templates show` | read | single_repo | local_file_authoring | real | no | no live state | Go embedded catalog read; CLI remains local authoring surface |
| `workflow.generate.preview` | web/chat preview | read | single_repo | not implemented in Python RPC | real | no | no live state | Go read-only planned-write preview |
| `list.runs` | `list runs` | read | single_repo | pg | real | no | no | stable |
| `list.sessions` | `list sessions` | read | single_repo | pg | real | no | no | stable |
| `list.jobs` | `list jobs` | read | single_repo | pg | real | no | no | stable |
| `list.artifacts` | `list artifacts` | read | single_repo | pg | real | no | no | stable |
| `artifact.show` | web artifact raw/detail DTO | read | single_repo | pg | real | no | no | stable |
| `list.workflows` | `list workflows` | read | single_repo | pg | real | no | no | stable |
| `worktree.list` | `worktree list` | read | single_repo | pg + git refs | real | no | no | read-only row projection includes worktree HEAD reachability, anchor kind, anchored ref, and checked refs |
| `dashboard.all` | `dashboard --all` | read | daemon_global | direct | real | no | no | Go/PostgreSQL read-only projection with per-active-run `run_progress` parity; remaining TODO 62 gaps are outside the dashboard-all run-progress slice |
| `repo.list` | `repo list` | read | daemon_global | pg repo registrar | real | no | no | bootstrap/admin |
| `repo.resolve` | client repository resolution | read | daemon_global | pg repo resolver | real | no | no | daemon-global bootstrap read for path -> repository_id resolution |
| `session.register` | `register-session` | claim | single_repo | pg | real | no | no | stable |
| `session.close` | `session close` | claim | single_repo | pg | real | no | no | stable |
| `session.report` | MCP pre-work session report | claim | single_repo | not implemented in Python RPC | real | no | no | RFC 0075 structured ready/heartbeat/question/escalate event path; updates RFC 0077 liveness timestamps; no terminal text authority |
| `wake.wait` | run-drive wake wait | read | single_repo | in-process notify hint + timeout fallback | real | no | no | RFC 0120 Phase 2 notify-only wait surface; wake payloads are hints over committed daemon state and never authorize transitions |
| `work.claim_next` | `claim-next` | claim | single_repo | pg | real | no | no | stable |
| `work.claim_override` | `work claim-override` | admin | single_repo | pg | real | no | no | #222 admin escape for the fresh-review process-lineage gate; claims a pending job for a session only when an accepted decision is scoped to the exact `(session_id, job_id)`; emits `work.claim_overridden`. No normal-lane `claim-next --force` |
| `work.await_packet` | MCP agent loop | claim | single_repo | not implemented in Python RPC | real | no | no | Go long-poll work-packet acquisition for autonomous MCP agents; records await and packet-delivery liveness timestamps; terminal `no_work` returns `idle_behavior=exit_session` per RFC 0120 / D180 |
| `work.ack` | `ack` | claim | single_repo | pg | real | no | no | stable |
| `work.heartbeat` | `heartbeat` | claim | single_repo | pg | real | no | no | stable; records RFC 0077 work-heartbeat activity |
| `work.release` | `release` | claim | single_repo | pg | real | no | no | stable |
| `supervise.start` | `supervise start` | claim | single_repo | pg + provider-auth preflight | real | no | no | Go process-control launch over PG supervisor rows and FIFO/helper transport; `provider_auth_gate` runs before supervisor rows, scratch, lane tokens, helper/tmux, or provider process launch |
| `supervise.send` | `supervise send` | claim | single_repo | pg | real | no | no | Go packet delivery with delivered-unacknowledged semantics |
| `supervise.rebridge` | `supervise rebridge` | claim | single_repo | pg + local tmux | real | no | no | Runtime Go RPC/CLI route that rebuilds the helper-owned tmux delivery bridge in place only when the recorded pane is live; never kills or respawns the pane |
| `supervise.report` | wrapper control report | claim | single_repo | pg | real | no | no | Go records direct control events and helper JSONL batches |
| `supervise.stop` | `supervise stop` | claim | single_repo | pg | real | no | no | Go terminal supervisor state update; tmux-backed lanes terminate the tmux session via RFC 0089 pane/session metadata |
| `supervise.status` | `supervise status` | read | single_repo | pg | real | no | no | read-only supervisor and protocol-liveness/stall projection; tmux-backed rows consult RFC 0089 tmux session/pane liveness; no pointer repair or lost-state mutation |
| `supervise.list` | `supervise list` | read | single_repo | pg | real | no | no | stable |
| `supervise.trajectory` | `supervise trajectory` | read | single_repo | pg + local scratch file | real | no | no | explicit read of operator-local `.striatum/scratch/<supervisor_id>/pty.log`; returns private diagnostics only and never treats terminal text as durable workflow provenance |
| `supervise.reattach_status` | supervisor reattach-status DTO | read | single_repo | pg | real | no | no | read-only reattach DTO; classifies tmux-backed rows with RFC 0089 tmux session/pane liveness |

`supervise.status` returns a `tmux` object for tmux-backed lanes, including
the copyable `attach_command`, `lane_backend`, `delivery_state`,
`pane_liveness`, `trajectory_log`, and derived `tmux.liveness` record. The class vocabulary is
`tmux_ok`, `tmux_session_missing`, `tmux_pane_missing`, `tmux_pane_dead`,
`tmux_pane_pid_mismatch`, and `tmux_unavailable`; the same strings feed
`lane_attestation_reason` on unhealthy tmux-backed lanes. The liveness record
also carries `state: healthy|degraded|lost` and a typed `probe_failure` record
on failures. A helper-owned attach-bridge exit with a live pane keeps pane
liveness attached/attested but surfaces `delivery_liveness.class=degraded` and
causes `supervise.send` to fail closed until `supervise.rebridge` rebuilds the
delivery bridge or the supervisor is restarted.
| `work.send_message` | `send` | write | single_repo | pg | real | no | no | stable |
| `work.block` | `block` | write | single_repo | pg | real | no | no | stable |
| `work.complete` | `complete` | write | single_repo | pg + git refs | real | no | no | repo-write jobs with active per-job worktrees anchor their HEAD by fast-forwarding the run branch or pinning `refs/striatum/<run_id>/<job_id>` before completion |
| `artifact.publish` | `publish-artifact` | write | single_repo | pg | real | no | no | stable |
| `repo.write` | `repo write` | write | single_repo | pg + local_file_authoring | real | no | no | exact-content mediated repository write; validates active session/lease and job write_scope before any filesystem mutation |
| `repo.patch_preview` | `repo patch-preview` | write | single_repo | pg + local_file_authoring | real | no | no | mediated unified-patch preview; validates active session/lease, `git apply --check`, and all changed paths against job write_scope without mutating files |
| `repo.patch_apply` | `repo patch-apply` | write | single_repo | pg + local_file_authoring | real | no | no | mediated unified-patch apply; repeats preview validation before applying and records changed-path metadata without patch text |
| `process.run` | `process run` | write | single_repo | pg + process execution | real | no | no | mediated command-array execution; requires active session/lease plus job `capability_requirements.process_execution=true` or a matching escape decision; records `process_executions` evidence and process events without durable stdout/stderr transcripts |
| `worktree.create` | `worktree create` | write | single_repo | pg | real | no | no | Go shells out to `git worktree add --detach` after PG lease/workflow validation |
| `worktree.release` | `worktree release` | write | single_repo | pg + git refs | real | no | no | refuses non-`--force` release while worktree HEAD is unreachable from the run branch or `refs/striatum/`; `--force` records `worktree.force_released`, including missing-on-disk terminal row retirement |
| `worktree.gc` | `worktree gc` | write | single_repo | pg + git refs + git worktrees | real | no | no | removes on-disk worktrees for terminal jobs whose HEAD is reachable from the run branch or `refs/striatum/`, and retires terminal rows whose path is already missing on disk; skipped rows are reported and removals emit `worktree.gc_removed` |
| `workflow.generate` | `workflow generate` | write | single_repo | local_file_authoring | real | no | no live state | Go generator writer; refuses unsafe paths/overwrites |
| `workflow.upgrade` | MCP/UI workflow upgrade; no current Go CLI route | write | single_repo | local_file_authoring | real | no | PG running-run guard only; no Go SQLite import | Registered daemon method only; the Python-era `workflow upgrade` CLI is retired. |
| `workflow.accept_risk` | `workflow accept-risk` / MCP/UI accepted-risk mutation | admin | single_repo | not implemented in Python RPC | real | no | no | Go append-only accepted-risk mutation; requires decision artifact reference, rationale, and lint finding fingerprint |
| `review.submit` | `submit-review` | review | single_repo | pg | real | no | no | stable |
| `review.verdict` | `verdict` | review | single_repo | pg | real | no | no | stable |
| `review.override` | `override-verdict` | admin | single_repo | pg | real | no | no | stable |
| `decision.record` | `decision record` | admin | single_repo | pg | real | no | no | stable |
| `checkpoint.resolve` | `checkpoint resolve` | admin | single_repo | pg | real | no | no | stable |
| `verifier.attest` | `verifier attest` (after local pin resolution) | admin | single_repo | pg | real | no | no | RFC 0141 / D243 (#482) operator-token attestation minter — the daemon-owned, gate-enforced PINNED→VERIFIABLE trust boundary. Writes the authoritative `striatumd.verifier_attestations` row binding (repository_id, check_id, binary_sha256). REFUSES any session-bound token (`capability_denied`): the verified lane can never bless its own pins. The run-completion gate (`evaluateRunClaimVerification`) refuses VERIFIED for an external claim whose backing receipt lacks an un-revoked attestation row, fail-closed to ASSERTED. The repo-file `allowlist.pins.<fp>.attest.json` sidecar is now a cache/projection, not the trust source |
| `escalation.list` | `escalation list`; `inbox` without `--session-id` | read | single_repo | pg | real | no | no | stable |
| `escalation.show` | `escalation show` | read | single_repo | pg | real | no | no | stable |
| `escalation.resolve` | `escalation resolve` | admin | single_repo | pg | real | no | no | stable |
| `branch.confirm` | `branch confirm` | admin | single_repo | pg | real | no | no | stable |
| `run.prepare` | `run prepare` | admin | single_repo | pg | real | no | no | stable |
| `run.start` | `run start` | admin | single_repo | pg | real | no | no | stable |
| `run.pause` | `run pause` | admin | single_repo | pg | real | no | no | stable |
| `run.resume` | `run resume` | admin | single_repo | pg | real | no | no | stable |
| `run.cancel` | `run cancel` | admin | single_repo | pg | real | no | no | stable |
| `run.retry_job` | `run retry-job` | admin | single_repo | pg | real | no | no | stable |
| `run.integrate` | `run integrate` | apply | single_repo | not implemented in Python RPC | real | no | no | Go RFC 0108 Phase 4 serialized gated integration (merge-tree plumbing; never auto-resolves) |
| `repo.init` | RPC/bootstrap helper; CLI uses `repo add --init` | admin | single_repo | bootstrap CLI helper | real | no | no Go SQLite import | Go registers PG-backed repo state and operational scratch only |
| `recovery.stale_leases` | `recovery stale-leases` | recovery | single_repo | pg | real | no | no | stable |
| `recovery.requeue_stale` | `recovery requeue-stale` | recovery | single_repo | pg | real | no | no | stable |
| `recovery.cancel_job` | `recovery cancel-job` | recovery | single_repo | pg | real | no | no | stable |
| `recovery.process_reconcile` | `recovery process-reconcile` | recovery | single_repo | pg | real | no | no | reconciles running process rows; tmux-backed rows consult RFC 0089 tmux session/pane liveness before marking lost |
| `recovery.resume` | `recovery resume` | recovery | single_repo | pg | real | no | no | stable |
| `recovery.sweep` | `recovery auto` | recovery | single_repo | pg | real | no | no | canonical one-shot recovery sweep; runs workflow-opt-in auto-finalize before lazy lease expiry |
| `recovery.auto_publish_stale_artifacts` | `recovery auto-publish` | recovery | single_repo | pg | real | no | no | explicit stale-artifact auto-publish |
| `recovery.auto` | deprecated alias | recovery | single_repo | pg alias | real | no | no | deprecated compatibility alias for stale-artifact auto-publish; current CLI does not emit it |
| `recovery.auto_finalize` | `recovery auto-finalize` | recovery | single_repo | pg | real | no | no | dry-run by default; Go handler registered; live mode requires workflow opt-in or force |
| `recovery.invalidate_job` | `recovery invalidate-job` | recovery | single_repo | pg | real | no | no | RFC 0118 P1-6 per-job invalidate; supersedes a compromised verdict under a scoped decision and reopens the job on a fresh attempt |
| `recovery.reseal` | `recovery reseal` | recovery | single_repo | pg | real | no | no | RFC 0125 P1-2 (D192); re-probes worktree-durability for a (run_id, job_id) and, on pass, requeues the SAME attempt (no attempt bump) so a remediated durability blocker completes without duplicating provenance |
| `recovery.complete_stalled` | `recovery complete-stalled` | recovery | single_repo | pg | real | no | no | GH #292 (D200); non-destructively completes a recovery-exhausted job whose required artifacts are already durable+reconstructable, resolving the recovery_exhausted blocker and restoring the run from needs_operator. Refuses verdict-capable jobs (RFC 0118). `--force` relaxes the recovery_exhausted-blocker precondition; `--dry-run` previews |
| `recovery.accept_quarantined` | `recovery accept-quarantined` | recovery | single_repo | pg | real | no | no | GH #311 P0; the operator action on a job the recovery decision tree quarantined when its run finalized-the-majority. Resolves the quarantined job's recovery_exhausted blocker + escalation and marks the job canceled-by-operator (terminal). Keys on (run_id, job_id); idempotent. Never completes the job or seals an artifact — the quarantined work was unrecoverable and is recorded honestly as canceled |
| `recovery.resolve_blocker` | `recovery resolve-blocker` | recovery | single_repo | pg | real | no | no | GH #304; closes a dangling open, non-escalation, non-checkpoint blocker by id (emits `blocker.resolved`) so the `open_blockers` frontier reconciles. Refuses `human_checkpoint`/`waiting_human` blockers (use `checkpoint resolve`) and escalation-class blockers (use `escalation resolve`); does not mutate run/job state |
| `recovery.prune_debris` | `recovery prune-debris` | recovery | single_repo | pg + git refs | real | no | no | GH #303; tombstones a terminal-debris (canceled/failed) run's unrecoverable artifact debris via append-only `recovery.debris_pruned` events (never a hard delete — `striatumd.artifacts` is owner-owned/append-only) so the doctor artifact-anchor pass suppresses it and reports `ok`. Eligibility is byte-identical to doctor (reuses the same classifiers); refuses non-terminal runs (`invalid_transition`). `--dry-run` previews; `--sweep-pins` also clears reachable `refs/striatum/` pins; idempotent |
| `recovery.quarantine_lane` | `recovery quarantine-lane` | recovery | single_repo | pg + git refs | real | no | no | GH #298; for a terminal (canceled/failed) run, snapshots a dirty lane worktree's uncommitted work to a durable, auditable `refs/striatum/quarantine/<run>/<job>/<attempt>` ref (daemon-owned write-tree + commit-tree, never disturbing the lane), records an append-only `recovery.lane_quarantined` event, then removes the worktree + retires its `job_worktrees` row. Closes the silent-data-loss where `worktree gc`/`release --force` discarded a dirty worktree. Refuses non-terminal runs (`invalid_transition`); a clean worktree is reported clean (and gc'd); `--dry-run` previews; idempotent. `worktree gc` now SKIPS a dirty worktree (`dirty_uncommitted_work`) and defers here instead of `--force`-discarding it |
| `apply.receipt.show` | n/a | read | single_repo | direct apply service | real | no | no | stable |
| `apply.receipt.verify` | n/a | read | single_repo | direct apply service | real | no | no | stable |
| `repo.add` | `repo add` | admin | daemon_global | pg repo registrar | real | no | no ordinary repo-local SQLite | bootstrap/admin |
| `repo.remove` | `repo remove` | admin | daemon_global | pg repo registrar | real | no | no | bootstrap/admin |
| `daemon.token.create` | `daemon token-create` | admin | daemon_global | not implemented in Python RPC | real | no | no | Go PostgreSQL token issuance; cleartext token returned once. CLI route added in #182 so operators can mint apply-capable tokens (run.integrate) without raw RPC |
| `daemon.token.revoke` | n/a | admin | daemon_global | not implemented in Python RPC | real | no | no | Go PostgreSQL token revocation by token id or full token |
| `daemon.token.rotate` | n/a | admin | daemon_global | not implemented in Python RPC | real | no | no | Go PostgreSQL token rotation with ambiguous-scope refusal |
| `daemon.key.rotate` | n/a | admin | daemon_global | not implemented in Python RPC | real | no | no | Go rotates the local Ed25519 sealed-apply fallback key file and returns key id/public-key metadata; full apply-gate mutation remains separate |
| `daemon.shutdown` | RPC only; stop service out of band | admin | daemon_global | daemon lifecycle helper | real | no | no | Go process-cancel hook returns accepted shutdown response; handler still fails closed only when embedded without a hook |
| `daemon.migrate` | RPC/admin migration method; CLI bootstrap helper is `daemon migrate-db` | admin | daemon_global | migration helper | real | no | no | Go applies embedded PostgreSQL migrations; no SQLite/Python dependency |
| `cross_repo.list` | `cross-repo list` | read | cross_repo | direct cross-repo service | real | no | no | stable |
| `cross_repo.describe` | `cross-repo describe` | read | cross_repo | direct cross-repo service | real | no | no | stable |
| `cross_repo.why` | `cross-repo why` | read | cross_repo | direct cross-repo service | real | no | no | stable |
| `cross_repo.cancel` | `cross-repo cancel` | recovery | cross_repo | daemon RPC + PG participant cancel | real | no | no | stable |

## Deprecated Alias Methods

These pre-RFC-0043 method names remain registered so older clients receive a
known response. They should not be emitted by current CLI routing.

| Alias method | Canonical method | Python authority | Go authority | CLI fallback | Status |
|---|---|---|---|---:|---|
| `ack` | `work.ack` | pg | real | no | deprecated |
| `heartbeat` | `work.heartbeat` | pg | real | no | deprecated |
| `release` | `work.release` | pg | real | no | deprecated |
| `block` | `work.block` | pg | real | no | deprecated |
| `complete` | `work.complete` | pg | real | no | deprecated |
| `publish_artifact` | `artifact.publish` | pg | real | no | deprecated |
| `claim_next` | `work.claim_next` | pg | real | no | deprecated |
| `verdict` | `review.verdict` | pg | real | no | deprecated |
| `submit_review` | `review.submit` | pg | real | no | deprecated |

## CLI-Only Or Out-Of-Band Commands

These commands are implemented by `go/pkg/cli/localcommands`,
`go/pkg/cli/rundrive`, or local CLI dispatch and are not standalone production
workflow mutation methods. When they affect workflow state, they do so by
calling daemon RPC methods.

| CLI command | Current authority | SQLite dependency | Classification |
|---|---|---|---|
| `skills install` | local filesystem installer | no workflow state | bootstrap_admin |
| `skills list` | embedded optional-skill catalog and on-disk manifest reader | no workflow state | bootstrap_admin |
| `plugin install` / `plugin uninstall` | local filesystem installer | no workflow state | bootstrap_admin |
| `daemon install` | renders systemd user unit and scaffolds `daemon.toml` | no workflow state | bootstrap_admin |
| `daemon uninstall` | disables/removes systemd user unit; leaves config/data intact | no workflow state | bootstrap_admin |
| `daemon status` | local unit/runtime layout plus read-only `striatum doctor` result | no workflow mutation | bootstrap_admin |
| `daemon migrate-db` | applies pending PostgreSQL migrations via owner/admin DSN | no workflow state | bootstrap_admin |
| `daemon owner-ddl apply` | applies owner-DDL bundles and reasserts protected grants | no workflow state | bootstrap_admin |
| `run drive` | local operator loop over `run.detail`, `list.sessions`, `session.register`, `supervise.start`, `supervise.stop`, and `session.close`; forwards `provider_auth_gate` to `supervise.start` | no direct workflow state writes outside daemon RPC | local_operator_loop |
| auto_spawn scheduler (daemon-internal, RFC 0122) | resident daemon loop over `session.register` + `supervise.start`, invoked under the captured **run-owner principal** read from a run-scoped spawn-authorization grant — never a synthetic principal. Refuses (`spawn_grant_missing`/`spawn_grant_expired`) without a valid, unexpired grant (C2); honors the paused-run hold and human-hold (non-`auto_spawn` lanes) via the shared reconcile predicate. Opt-in per deployment (`--auto-spawn-scheduler`) on top of the per-lane `supervision.auto_spawn` opt-in | no direct workflow state writes outside daemon RPC (replays the same handlers a client would) | daemon_initiated_spawn |
| `workflow validate` | offline workflow JSON validation | no live state | local_file_authoring |
| `workflow generate` | embedded catalog scaffold preview/write helper | no live state | local_file_authoring |
| `workflow templates list` / `workflow templates show` | embedded workflow-template catalog reads | no live state | local_file_authoring |
| `verifier run` | RFC 0134 / D227 lane-side executable verifier: resolves a content-addressed, operator-curated allowlist entry (`go/pkg/verifier`), runs the check TWICE under the strictest available sandbox (bubblewrap / systemd-run / unshare+ulimit, no-network + no-new-privileges + read-only-except-scratch + cgroup/cpu/mem/wall-clock caps), and mints a tamper-evident `receipt.v1`. It is the ONLY command in the feature that executes a check, and it runs INSIDE the disposable verifier lane — never on the daemon's gate path. No daemon RPC, no live state | no live state | local_verifier_lane |

## Immediate Findings

1. Handwritten daemon fallback route tables are gone. Runtime CLI route
   translation now comes from the contract `cli_routes` map plus CLI-local
   parameter extraction, and production route-layer failures fail closed.
2. `recovery.sweep` is now the canonical RFC 0020 one-shot recovery
   sweep emitted by `striatum recovery auto`. `recovery auto-publish`
   emits the explicit `recovery.auto_publish_stale_artifacts` method.
   `recovery.auto` remains only as a deprecated compatibility alias for
   older stale-artifact auto-publish clients. `striatum recovery watch`
   is CLI-local scheduler glue over `recovery.sweep`, not a registered
   `recovery.watch` RPC method.
3. `repo.add`, `repo.list`, and `repo.remove` now route through daemon RPC
   and register against `striatumd.repositories` without opening or creating
   `.striatum/retired-local-state`; `--init` creates only operational scratch.
4. `repo.resolve` is a daemon-global bootstrap read because repository-scoped
   authorization cannot know the repository id before resolution. Current Go
   CLI, MCP, and web clients resolve repositories through daemon RPC.
5. Go daemon routes fail closed for SQLite-bound dogfood composites.
   Operators should use primitive daemon methods (`work.ack`,
   `artifact.publish`, `review.verdict`, `work.complete`, and ordinary
   `recovery.*`) until a PostgreSQL-native composite is designed.
6. Go daemon startup now owns the resident active-run recovery scheduler:
   it calls Go `recovery.sweep`, records `daemon.recovery_sweep`, and upserts
   `striatumd.scheduler_cursors` without production SQLite.
6a. RFC 0122 (#212) adds a second resident loop, the **auto_spawn scheduler**
   (opt-in via `--auto-spawn-scheduler`). It is a NON-CLIENT invoker of the
   `session.register` + `supervise.start` mutation path: where those methods
   are otherwise reached only through a client capability, the scheduler reaches
   them under the **run owner's pre-authorization** — a durable, run-scoped,
   revocable `spawn_authorization_grant` captured at `run.start` and replayed by
   setting `striatum.principal_id` to the captured owner (attestation identical to
   a manual `supervise.start`, C1). The contract it must hold: the scheduler
   cannot spawn without a valid, unexpired, run-scoped grant — a missing/expired/
   revoked grant is a loud refusal (`spawn_grant_missing`/`spawn_grant_expired`),
   never a silent fallback (C2). The guardrail cases live in
   `go/pkg/mutations/scheduler_test.go`
   (`TestSchedulerRefusesExpiredGrant`/`TestSchedulerRefusesMissingGrant`), with
   attestation parity (`TestSchedulerSpawnAttributionUsesOwnerPrincipal`),
   double-spawn (`TestSchedulerNoDoubleSpawn*`), and the paused/human holds
   (`TestSchedulerRespectsHold*`).
7. `/v1/invoke` routes daemon-mapped production reads and allowed mutations
   through daemon RPC. MCP `tools/list` / `tools/call` are derived from the
   daemon method registry and do not advertise CLI-shaped aliases. Hidden local
   workflow-authoring methods fail closed in Go MCP `tools/call` with
   `tool_hidden`, including for write-capable tokens.
8. The legacy Python SQLite engine is deleted from the production tree.
   Historical SQLite references remain only in archived docs and fixtures; live
   workflow state is daemon-owned PostgreSQL.
9. Go no longer has generic `not_implemented` handlers for active contract
   methods. D110 removed the SQLite-bound dogfood composites and the
   repo-local SQLite import RPC from the production contract; D112 removed
   `apply.reviewed_patch`. Removed names stay absent from the registry and
   stale MCP/RPC calls return `method_unknown`; contract, architecture, and
   MCP/RPC tests pin that behavior. Web/service DTO parity gaps are tracked
   separately under RFC 0069-0071.
10. Daemon MCP resources (`resources/list` and `resources/read`) use
    PostgreSQL-backed repository visibility and read projections. A missing
    daemon PostgreSQL connection fails closed; the legacy registry-backed
    no-`pg_conn` fallback is retired.
11. Capability tokens may be bound to a single session (RFC 0096 V2 / GH #135).
    The authorizer surfaces the grant's `session_id` on the resolved
    `AuthContext` (`client_capabilities.session_id`; NULL = session-unbound),
    and `rpc.Server.handle` threads that `AuthContext` onto the request context
    after authorization. Session-scoped handlers
    (`interrogation.open`/`ask`/`answer`/`close`) enforce it: a session-bound
    token may act ONLY as its own session (else `capability_denied`), while an
    unbound operator/coordinator token is still allowed but records honest
    provenance — for `interrogation.answer` the turn and `interrogation.answered`
    event carry `responder=operator` / `operator_override=true` rather than
    falsely attributing the answer to the target lane. `session.register` mints
    a session-bound token, and supervised lane launch injects that lane's own
    bound token instead of the shared operator override. `AllowAllAuthorizer`
    yields an unbound `AuthContext`, so dev/test wiring exercises the
    operator-override path, never a bound-session bypass. Session-scoped work,
    artifact, and review mutations enforce the same binding guard; when a
    closed predecessor token targets the active successor session in the same
    run/role/lane slot, the daemon returns `session_token_stale` with
    restart/rebridge remediation instead of silently accepting stale authority.

## Error code catalog (RFC 0111)

Every error code the daemon RPC surface (and its CLI client shim) can return,
as a closed contract. The authoritative registry is
`go/pkg/rpc/error_catalog.go`; `go/pkg/rpc/error_catalog_test.go`
guard-reconciles it in both directions against the error-code literals in the
Go source (`NewError("…")`, `Code: "…"`, and capability `DenialReason`
values, which `RequireAllowed` converts verbatim into `rpc.Error` codes) and
fails when this section is missing a cataloged code. `ErrorResponse` centrally
fills `suggestion` from this catalog when a call site sets none (explicit
call-site suggestions win); the MCP boundary renders code, message, and
suggestion into the `tools/call` content text and `structuredContent`
(`error` / `error_message` / `suggestion`). A `—` suggestion means no generic
remediation is sensible for that code.

| Code | Meaning | Default suggestion |
|---|---|---|
| `artifact_error` | An artifact operation (publish, read, validation, or flag contract) failed. | Read the message for the failing artifact constraint, fix the artifact (front matter, paths, required flags), and retry the publish. |
| `audit_append_failed` | The daemon could not append the audit row, so the operation was refused or rolled back (fail-closed provenance). | Retry once; if it persists, check daemon PostgreSQL health with `striatum doctor` and report the audit_id. |
| `autonomous_worktree_isolation_required` | A supervised or agent-loop repo-write lane is configured to use the shared checkout without a recorded interactive-human compatibility override. | Set worktree_isolation: per_job on the repo-write lane, or set allow_shared_checkout_repo_write=true with a non-empty shared_checkout_repo_write_rationale for an explicit interactive-human compatibility workflow. |
| `bad_host` | The MCP endpoint rejected a request whose Host header is not loopback. | Call the daemon MCP endpoint via its loopback address exactly as provided in STRIATUM_MCP_URL. |
| `bad_origin` | The MCP endpoint rejected a browser-style request whose Origin header is not loopback. | Send requests from a loopback origin or drop the Origin header. |
| `base_head_mismatch` | The current git HEAD does not match the commit_request base_head. | Regenerate the commit_request against the current HEAD, then re-run git.commit_apply. |
| `barrier_blocked` | A sealed expectation barrier (RFC 0135) cannot fire because a live in-edge is a blocking contribution or an unresolvable seat (BARRIER_BLOCKED), not a clean terminal gap. | Resolve the blocked seat(s) listed in blocked_manifest (clear the blocker, complete or recover the seat), or recover the run; then re-run `striatum join verify <barrier-id>`. |
| `barrier_integrity_failed` | A sealed expectation barrier (RFC 0135) failed integrity verification: its manifest does not match the staged refs at the live seal, or its assembly journal is inconsistent (unreachable target, committed-manifest mismatch, or terminal failure). | Inspect the problems and manifest in the verify result; recover the assembly through the daemon (do not hand-finish), then re-run `striatum join verify <barrier-id>`. |
| `barrier_smuggled_content` | A fan-in staging contribution smuggles content into the join (RFC 0133 Risks / #352, #353): a merge commit in its chain grafts an off-base side branch, the frozen tip's tree no longer matches the sealed frozen_tip_tree_sha, or the contribution does not descend from the frozen base AND is a contaminated base rather than a recoverable drift (disjoint history, or an off-base foreign root the frozen base does not share). A legitimate base drift (the run branch evolved under the sibling's feet, sharing a real merge-base and no foreign root) is recovered as an extra-parent leg, not refused; this code marks the cases that cannot be recovered. | Re-author the contribution on top of the frozen base or its evolved lineage (no merge of an off-base / disjoint branch) and re-stage; if the frozen tip's tree was re-pointed, the run is exposing a corrupted base — recover it through the daemon, do not hand-finish. |
| `blob_apply_required` | The blob bucket does not exist and creation was not authorized. | Re-run `striatum repo add <path> --apply-blob-creation` to create the bucket. |
| `blob_disabled` | The daemon is not configured for blob storage. | — |
| `blob_head_failed` | The blob backend failed to stat an object. | — |
| `blob_list_failed` | The blob backend failed to list a bucket. | — |
| `blob_provision_failed` | The blob backend failed to provision the repository bucket. | — |
| `blob_publish_failed` | Uploading an artifact body to the blob backend failed (including post-upload sha256 mismatch). | — |
| `blob_read_failed` | Reading an object from the blob backend failed. | — |
| `branch_confirmation_required` | The run branch is not confirmed (or the run is not started), so claims are refused. | Confirm the run branch and start the run (`striatum run start --run-id <id> --branch <name>`) before claiming work. |
| `branch_mismatch` | The current git branch does not match the commit_request branch. | Check out the branch named in the commit_request, then retry. |
| `capability_denied` | The token is valid but this session may not perform the requested action (for example: not the floor holder, interrogator, or target session). | Verify you are the session the action belongs to and re-issue the call from that session; do not act for other lanes. |
| `capability_expired` | The granted capability has expired. | Re-register the session (session.register) or ask the operator to mint a fresh capability token, then retry. |
| `capability_missing` | The token does not carry the capability the method requires. | Use a token that grants the required capability the error names: re-register the session, or have an admin mint one with `striatum daemon token-create --capability <name>` (see docs/how-to/how-to-human.md). |
| `capability_scope_mismatch` | The capability is scoped to a different repository than the request targets. | Re-issue the call with the repository_id the token is scoped to, or obtain a token scoped to this repository. |
| `commit_request_not_found` | The referenced commit_request artifact does not exist or is not readable. | Publish the commit_request artifact first, then retry with its request_id. |
| `concurrent_run_isolation_required` | Another run is already active on the repository and this run has a repo-write job on a lane without worktree_isolation: per_job, so starting it would share the main checkout (RFC 0108 Phase 2). | Set worktree_isolation: per_job on the run's repo-write lane so each run gets its own detached worktree, then start the run; or wait for the active run to finish. |
| `confirmation_required` | A mutating verb needs an explicit confirmation that was not supplied or did not match. | Re-run with the explicit confirmation the message names (for example confirm=true with a matching confirm_request_id). |
| `conflict` | A uniqueness or attribution conflict (for example a client already attributed to another principal). | — |
| `cross_run_collision` | Starting this run collides with another active run on the repository — they target the same git branch (RFC 0108 Phase 3). | Give this run a distinct branch (each parallel run integrates on its own branch), or pass --allow-overlap to start anyway and resolve the overlap at integration. |
| `daemon_auth_lost` | The daemon's authority secret no longer matches the database registry (the row is missing or was superseded by a concurrent rotator), so an authorized write was refused (RFC 0110 §4.5). | Restart the daemon to re-bootstrap its authority, or check for a concurrent rotator on the same runtime role (use per-instance roles for a shared PostgreSQL). |
| `daemon_db_missing` | The operation requires daemon PostgreSQL, which is not configured or reachable. | Check daemon PostgreSQL health with `striatum doctor` and restore the database before retrying. |
| `daemon_under_load` | The operation timed out behind transient daemon back-pressure (a statement_timeout/57014 or lock_timeout/55P03 event-append/lifecycle convoy under multi-run supervise load) rather than a real refusal, after the daemon already retried it (#198/#355/#389/#383). | Retry the operation shortly; if it persists, check daemon PostgreSQL load with `striatum doctor` and look for a long-held lock on repo_event_chain_heads. |
| `daemon_unreachable` | The CLI could not reach the daemon RPC socket. | Ensure striatumd is running (`striatum doctor` reports daemon health), then retry. |
| `dirty_tree_outside_commit_request` | The working tree has changes outside the commit_request included_paths. | Commit or revert the changes outside included_paths, then retry. |
| `displaced_session_live` | session.register --replace would displace a session that has heartbeated within the lease's heartbeat window, so it is still live and may be actively driving the same work packet (#189). | Confirm the displaced session is genuinely wedged (check `striatum list sessions` and recent heartbeats); if so, retry with --force-live --reason "..." to record why the live lane is being superseded, or close it first with `striatum session close <id>`. |
| `duplicate_request` | The RPC request_id was already used. | Re-issue the call with a fresh request_id. |
| `event_payload_rejected` | A durable event payload was refused by the database write boundary: it carried a transcript key (stdout/stderr/transcript/raw_output/provider_output) or exceeded the durable-event size cap (RFC 0110 §12, C-EVENT-NO-TRANSCRIPTS). | Record curated coordination state in the event, not captured agent output; transcripts belong in operator-local diagnostics, not the durable event chain. |
| `file_read_failed` | The daemon could not read a repository file it was asked to operate on. | Verify the path exists and is readable inside the repository, then retry. |
| `fresh_review_byte_identical` | A review.submit finding is byte-identical (content_sha256) to this job's prior-attempt finding, so it re-asserts a stale verdict against a revised target instead of a fresh review (#206). | Delete the stale finding file left at the artifact path by the prior round, read the CURRENT revision of the target, and write your own finding before resubmitting. |
| `git_commit_apply_failed` | A git step of commit apply failed. | — |
| `git_snapshot_failed` | Capturing the git snapshot failed. | — |
| `git_unavailable` | The git executable is not available to the daemon. | Install git and ensure it is on the daemon's PATH. |
| `internal_error` | An unexpected daemon-side failure that does not map to a stable code. | Retry once; if it persists, report the failure with its audit_id. |
| `invalid_transition` | The requested state transition is not legal from the current job, run, lease, or session state. | Re-read the live state (job.detail / run.detail, or `striatum status`) and take only the transition the current state allows. |
| `key_rotation_unavailable` | daemon.key.rotate is not wired in this daemon build; signing keys were not modified. | — |
| `lane_provider_auth_failed` | The lane provider-auth preflight found missing, stale, expired, revoked, or unrefreshable provider credentials for the lane identity. | Refresh the provider login for the lane OS user, then retry supervise.start. |
| `lane_provider_binary_missing` | The lane provider-auth preflight could not find or execute the provider CLI under the lane launch environment. | Install the provider CLI for the lane launch environment or add the binary directory to lane path_prefix. |
| `lane_provider_preflight_launch_failed` | Striatum could not start the closed provider-auth smoke command under the intended lane identity. | Fix lane run-as user, sudo, home directory, and launch environment provisioning. |
| `lane_provider_preflight_timeout` | The lane provider-auth preflight timed out, including hung refresh paths or interactive prompts. | Inspect the lane provider login for an interactive prompt or hung refresh path. |
| `lane_provider_preflight_unexpected_result` | The lane provider-auth smoke reached an unsupported result shape that cannot be classified as auth success, auth failure, launch failure, timeout, binary missing, or provider unavailable. | Inspect the provider CLI manually; the smoke completed with an unsupported result shape. |
| `lane_provider_preflight_unsupported` | The selected provider-auth gate mode requires a provider or lane shape that has no supported smoke. | Use --provider-auth-gate off or configure a provider with a supported auth preflight. |
| `lane_provider_unavailable` | Network, provider service, rate limit, or provider-side availability prevented the lane provider-auth preflight from reaching an auth conclusion. | Retry after provider or network availability recovers. |
| `lease_error` | The supplied lease is missing, expired, inactive, owned by another session, or bound to a different job. | Heartbeat your lease (work.heartbeat); if it is stale, recover stale leases (`striatum recovery stale-leases`) and re-claim via work.await_packet. |
| `merge_conflict` | Integrating a run's branch into the target mainline conflicts; the merge was refused and mainline left untouched (RFC 0108 Phase 4 never auto-resolves). | Rebase or resolve the run branch against the target on a branch a maintainer merges, then re-run run.integrate; the conflicting paths are in the error details. |
| `method_unknown` | The method has no registered handler. | Call tools/list and use a method the daemon actually exposes. |
| `not_found` | A referenced entity (session, job, artifact, interrogation, ...) does not exist. | List the live entities first (list.runs / list.jobs / list.sessions / artifact.list_for_run) and re-issue with an id that exists. |
| `not_implemented` | The method is registered but not implemented in this daemon build. | — |
| `path_conflict` | The active repository path is occupied by a different repository identity. | — |
| `path_outside_scope` | The path escapes the allowed scope (write scope, export directory, or repository root). | Use a path inside your packet's write_scope.allowed_paths (or the allowed output directory) and retry. |
| `path_traversal` | Path traversal outside the repository was refused. | Use a repository-relative path without `..` segments. |
| `receipt_missing` | The apply receipt was not found. | — |
| `repo_blob_conflict` | The repository's blob bucket is owned by a different repository identity. | — |
| `repo_not_found` | The repository path does not exist on disk. | Verify the repository path and re-run `striatum repo add` with the correct location. |
| `repo_not_registered` | The repository is not registered with the daemon. | Register the repository first (`striatum repo add`), then retry. |
| `repo_scratch_missing` | The repository scratch area is not initialized. | Register the repository with `striatum repo add --init`, then retry. |
| `review_provenance_override_required` | An unattested/operator-authored accepting review verdict requires an explicit run-level review provenance decision. | Record an accepting decision with `--escape-surface review_provenance --escape-action <action> --rationale <reason>`, then retry with `--review-provenance-decision-id <decision_id>`. |
| `run_not_found` | The run_id was not found. | List runs (list.runs) and use an existing run_id. |
| `schema_invalid` | The request failed schema validation (missing, ill-typed, or malformed parameters or envelope). | Fix the named parameter to match the documented schema and resend the request. |
| `session_inactive` | A terminal or inactive session tried to publish or complete work after losing authority for the lane. | Recover through daemon state: requeue the job on the same attempt (`striatum recovery requeue-stale --run-id <run_id> --job-id <job_id> --force --justification "..."`), then register or supervise.start a fresh session and retry from that fresh session. If the session is still active but about to be closed, use `striatum session close --session-id <session_id> --reason "..." --requeue-job`. |
| `session_token_stale` | A session-bound token belongs to a closed predecessor session but was used to act as that lane's active successor session. | Stop the stale lane and run supervise.start for the active successor session so it receives its own session-bound token; use supervise.rebridge only to repair that successor's existing supervisor, then retry from the successor lane. |
| `sha256_mismatch` | A file body sha256 does not match the published artifact's content_sha256 (the repo file drifted). | Re-publish the artifact from the current file (artifact.publish) or restore the file to the published content before retrying. |
| `shutdown_unavailable` | daemon.shutdown is not wired in this daemon process. | Stop the daemon with its service manager or a signal instead. |
| `signing_key_insecure` | The sealed-apply signing key fails security requirements (for example permissions). | — |
| `signing_key_invalid` | The sealed-apply signing key is invalid or unusable. | — |
| `spawn_grant_expired` | The run's spawn-authorization grant has expired, so the daemon auto_spawn scheduler refuses to spawn under a stale grant (RFC 0122 C2). | Re-authorize the run by restarting it (run.start re-captures a fresh grant), or drive the run manually. |
| `spawn_grant_missing` | An auto_spawn run has queued lane work but no active spawn-authorization grant; the daemon scheduler cannot invent authority (RFC 0122 C2). | Re-run run.start to capture a grant, or drive the run manually with `run drive`. |
| `spawn_grant_no_owner_principal` | An auto_spawn run.start had no authenticated owner principal to capture, so there is no identity for the scheduler to replay. | Authenticate run.start with a capability token that carries a principal, then retry. |
| `spawn_run_as_unresolved` | An auto_spawn run's run-as identity (the configured lane OS user) cannot be resolved on this host, so the scheduler would spawn into a non-existent identity (RFC 0122 §4). | Provision the lane OS user (with a home directory) or unset STRIATUM_LANE_OS_USER to run lanes as the daemon user, then restart the run. |
| `stale_daemon_identity` | The MCP request presented a boot epoch that does not match the live daemon's, so it dialed a recycled port now bound by a different daemon process run and was refused before touching run state (#316). | Relaunch the lane against the current daemon (re-run supervise.start, or recover the stalled lane) so it carries the live daemon's boot epoch; do not reuse a stale on-disk MCP endpoint/config pin. |
| `symlink_refused` | A symlinked path was refused (repository registration and scoped writes resolve real paths). | Use the real (non-symlinked) path and retry. |
| `target_unavailable` | The target session for the requested operation does not exist or is unavailable. | List live sessions (list.sessions) and target one that is active. |
| `token_expired` | The capability token is expired. | Obtain a fresh capability token (re-register the session or ask the operator), then resend with the new token. |
| `token_invalid` | The capability token does not exist or its secret does not match. | Resend with a valid capability token exactly as issued; if yours was rotated, obtain a fresh one from the operator or session registration. |
| `token_malformed` | The capability token is not in the issued token_id.secret form. | Send the capability token exactly as issued (token_id.secret) in the Authorization bearer header. |
| `token_missing` | No capability token was supplied on a method that requires one. | Send your capability token as an Authorization bearer header (lane environments provide STRIATUM_MCP_TOKEN). |
| `token_revoked` | The capability token has been revoked. | Obtain a fresh capability token (re-register the session or ask the operator), then resend with the new token. |
| `token_scope_ambiguous` | The token carries duplicate active capability scopes, so the daemon cannot pick one. | — |
| `token_unavailable` | The CLI could not load a capability token from its configured token file. | Run inside a workflow lane (which provides the token) or point the CLI at a readable capability-token file. |
| `version_incompatible` | The client and daemon share no supported envelope version. | Upgrade so client and daemon match (`make install`, then restart striatumd so the running image is the new build). |
| `write_scope_drift` | A job attempted to publish or complete work outside the frozen write scope for its current attempt. | Use the path in the active work packet, clear or move out-of-scope changes, or route through audited recovery (`striatum recovery resume` for remediated write-scope blockers, or a fresh/replacement attempt for legitimate scope changes). |
| `worktree_head_unreachable` | worktree.release refused because the worktree HEAD is not reachable from the run branch or a refs/striatum pin. | Complete the job so work.complete anchors the commits, or rerun worktree release with --force only if discarding that HEAD is intentional. |
| `worktree_required` | A repo-write job on a lane with `worktree_isolation: per_job` tried to publish, write, patch, run a process, or complete without an active job worktree. | Run worktree.create using the active session, job, and lease from the work packet, then retry the operation. |
| `workflow_error` | Workflow validation, preparation, or run orchestration failed. | — |
| `workflow_snapshot_not_found` | The workflow_snapshot_id was not found. | Read the run's workflow_snapshot_id from run.detail and re-issue with it. |
