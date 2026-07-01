package routes

import (
	"fmt"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/cli/params"
)

// Param describes one positional or flag input for a CLI verb, so `--help`
// can list required AND optional inputs instead of forcing the operator to
// discover them as runtime "<method> requires <param>" errors (issue #63 F9).
type Param struct {
	// Name is the canonical wire/flag name in kebab-case (e.g. "session-id",
	// "reason", "capability"). Positionals reuse the same name.
	Name string
	// Positional is true when the value may be supplied positionally (it can
	// always also be supplied as --name value).
	Positional bool
	// Required is true when the daemon rejects the call without this input.
	Required bool
	// Repeatable is true when the flag may be passed more than once
	// (e.g. --capability).
	Repeatable bool
	// Bool is true when the flag is a presence flag that takes no value
	// (e.g. --fresh).
	Bool bool
	// Values lists the accepted enum values when the input is constrained
	// (e.g. action: continue|cancel).
	Values []string
	// Help is a short human description.
	Help string
}

// Usage is the discoverability descriptor for a single CLI verb.
type Usage struct {
	Params []Param
	// Notes are extra free-form lines shown under the flag list (e.g. to call
	// out the register-session naming history).
	Notes []string
	// Generated is true when this descriptor was synthesized from the params
	// package's positional table (issue #194) rather than hand-curated. A
	// generated descriptor enumerates positional arguments and explicitly says
	// optional flags are derived from the daemon method, because no
	// machine-readable schema of optional flag names exists — the daemon
	// handlers read them imperatively.
	Generated bool
}

// usageByGroup maps a route ParamsGroup to its discoverability descriptor.
// Only groups with an entry here render a full `--help`; others fall back to a
// generic synopsis derived from the route metadata.
var usageByGroup = map[string]Usage{
	"status": {
		Params: []Param{
			{Name: "run-id", Help: "scope the snapshot to a single run; omit for the repo-wide view"},
			{Name: "all-runs", Bool: true, Help: "include every historical run in the repo-wide view (default: active runs + the most recent terminal runs)"},
			{Name: "run-limit", Help: "cap on recent terminal runs in the repo-wide default (default: 20; ignored with --all-runs or --run-id)"},
		},
		Notes: []string{
			"By default the repo-wide view returns every active run plus the most recent terminal runs, and claimable_jobs / blocked_downstream_jobs exclude terminal runs (#193). Use --all-runs for the full history or --run-limit N to widen the terminal-run window.",
		},
	},
	"doctor": {
		Params: []Param{
			{Name: "verbose", Bool: true, Help: "include structured problem_records alongside the stable problems string list"},
			{Name: "lane-provider-auth", Values: []string{"codex", "claude"}, Help: "explicit opt-in provider-auth diagnostic for a lane provider; ordinary doctor and doctor --verbose do not run provider CLIs"},
			{Name: "run-id", Help: "optional run whose frozen workflow lane should supply binary/path_prefix for the provider-auth diagnostic"},
			{Name: "lane-id", Help: "optional lane in --run-id whose provider binary/path_prefix should be used"},
			{Name: "timeout", Help: "provider-auth diagnostic timeout; defaults to 45s"},
		},
		Notes: []string{
			"Provider auth preflight is explicit-only: pass --lane-provider-auth codex or --lane-provider-auth claude, normally with --json. Codex may use network/provider tokens; Claude checks resolver-selected credential freshness offline.",
		},
	},
	"join_verify": {
		Params: []Param{
			{Name: "barrier-id", Positional: true, Required: true, Help: "the sealed expectation barrier (RFC 0135) to verify"},
		},
		Notes: []string{
			"Read-only. Verifies the barrier's manifest matches the staged refs at the live seal and its assembly journal is consistent; exits non-zero (barrier_integrity_failed / barrier_blocked) on a corrupted or blocked barrier, so it is usable as a CI/operator gate.",
		},
	},
	"corpus_export": {
		Params: []Param{
			{Name: "run-id", Positional: true, Help: "optional run id used by additive feedstock classes such as lane_trajectory"},
			{Name: "out", Positional: true, Help: "legacy output path argument retained for CLI compatibility"},
			{Name: "redaction-tier", Values: []string{"public", "curated", "internal"}, Help: "redaction tier to apply; defaults to public"},
			{Name: "limit", Help: "maximum artifact rows; defaults to 1000"},
			{Name: "include-lane-trajectory", Bool: true, Help: "include the default-off redacted lane_trajectory feedstock class"},
		},
		Notes: []string{
			"Without --include-lane-trajectory, corpus.export returns the existing artifact-only corpus rows.",
		},
	},
	"why": {
		Params: []Param{
			{Name: "target-id", Positional: true, Required: true, Help: "id to explain — a run_id, job_id, session_id, message_id, lease_id, or blocker_id"},
		},
		Notes: []string{
			"why resolves the target_id and prints how it reached its current state; list runs with `striatum list runs` and find ids with `striatum status --run-id <id>`.",
		},
	},
	"run_prepare": {
		Params: []Param{
			{Name: "workflow", Positional: true, Required: true, Help: "path to the workflow JSON to prepare a run from (also accepts --workflow-path)"},
		},
		Notes: []string{
			"prepare freezes a workflow snapshot and creates a `ready` run; it does not start lanes. Start the prepared run with `striatum run start --run-id <id>`. To list runs use `striatum list runs`.",
		},
	},
	"run_start": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "the prepared (`ready`) run to start; also accepts --run-id"},
			{Name: "allow-overlap", Bool: true, Help: "permit starting despite an overlapping write_scope with another active run (RFC 0108 Phase 3); a same-branch collision is still refused"},
			{Name: "no-drive", Bool: true, Help: "CLI-only: do not auto-launch the detached `run drive` driver after start (also: STRIATUM_RUN_DRIVE_AUTO=0)"},
		},
		Notes: []string{
			"Unless --no-drive (or STRIATUM_RUN_DRIVE_AUTO=0) is set, a successful start auto-launches a detached `run drive` so the run reconciles to terminal with no operator in the loop. The transient driver does NOT survive a daemon/DB restart — after a `run pause` + restart + `run resume`, re-arm the driver with `striatum run drive --run-id <id>` unless the daemon auto_spawn scheduler is enabled for the run (run.resume reports which path applies).",
		},
	},
	"run_pause": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "the active run to pause; also accepts --run-id"},
			{Name: "reason", Help: "human-readable pause reason recorded on the run; defaults to operator_paused"},
		},
		Notes: []string{
			"Pause holds the run: the auto_spawn scheduler launches no new lanes while paused_at is set. In-flight lanes are not killed. Resume with `striatum run resume --run-id <id>`.",
		},
	},
	"run_resume": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "the paused run to resume; also accepts --run-id"},
		},
		Notes: []string{
			"Resume only lifts the pause hold (clears paused_at) — it does NOT itself restart the driving loop. It returns a `drive` field naming which home re-drives the run: `daemon_auto_spawn` (the RFC 0122 scheduler re-adopts it automatically), or `operator_run_drive` / `auto_spawn_scheduler_disabled` (you must re-invoke `striatum run drive --run-id <id>`; the transient driver from `run start` does not survive a daemon/DB restart). The result's `next_action` is the exact command to run when one is required.",
		},
	},
	"claim_next": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "active session that should claim the next eligible work packet"},
			{Name: "lease-seconds", Help: "lease duration for the claim; defaults to 3600 seconds"},
		},
	},
	"recall_search": {
		Params: []Param{
			{Name: "query", Positional: true, Required: true, Help: "artifact-metadata full-text query"},
			{Name: "limit", Help: "maximum hits to return; defaults to 8 and caps at 20"},
		},
		Notes: []string{
			"recall.search is a read-only local Postgres FTS over artifact metadata; it does not call the warm tier and is not a state transition.",
		},
	},
	"work_claim_override": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session to claim the job for, authorized by the decision"},
			{Name: "job-id", Positional: true, Required: true, Help: "pending job the decision authorizes this session to claim"},
			{Name: "decision-id", Positional: true, Required: true, Help: "accepted decision (scoped to the exact session_id + job_id) authorizing the override"},
			{Name: "lease-seconds", Help: "lease duration for the claim; defaults to 3600 seconds"},
		},
		Notes: []string{
			"Admin-only escape for the #222 fresh-review process-lineage gate. Requires an accepted decision recorded with --subject-session-id/--subject-job-id matching exactly; a broad or mismatched decision is refused. There is no normal-lane claim-next --force.",
		},
	},
	"work_packet_show": {
		Params: []Param{
			{Name: "packet-id", Positional: true, Help: "specific work packet id to inspect"},
			{Name: "message-id", Help: "filter packets by queue message id"},
			{Name: "job-id", Help: "filter packets by job id"},
			{Name: "session-id", Help: "filter packets by receiving session id"},
			{Name: "run-id", Help: "filter packets by run id"},
			{Name: "limit", Help: "maximum packet rows to return; defaults to 20 and caps at 200"},
			{Name: "raw", Bool: true, Help: "include packet_json in the response; omitted by default to avoid leaking task prose"},
		},
		Notes: []string{
			"At least one selector is required. Without --raw, this returns metadata and packet_sha256 only.",
		},
	},
	"records_docket": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "run whose artifact/generated-record docket should be rendered"},
			{Name: "format", Values: []string{"markdown", "json"}, Help: "docket body format; defaults to markdown"},
		},
		Notes: []string{
			"Read-only. Renders a compact RFC 0171 docket from daemon-indexed artifact metadata and generated_records rows; markdown output prints directly unless --json is used.",
		},
	},
	"ack": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session that claimed the packet"},
			{Name: "message-id", Positional: true, Required: true, Help: "claimed work message id from the packet"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease id from the packet"},
		},
	},
	"heartbeat": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session that owns the active lease"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease to extend"},
			{Name: "extend-seconds", Help: "new lease extension window; defaults to 1800 seconds"},
		},
	},
	"release": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session that owns the active lease"},
			{Name: "message-id", Positional: true, Help: "claimed work message id; optional when --lease-id is supplied"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease to release"},
			{Name: "reason", Help: "release reason recorded on the lease; defaults to released"},
			{Name: "requeue", Bool: true, Help: "return non-repo-write work to the queue on the same attempt"},
			{Name: "transfer", Bool: true, Help: "operator-inspected repo-write transfer that returns work to the queue on the same attempt"},
		},
		Notes: []string{
			"When omitting message-id, pass --lease-id explicitly; positional release arguments are session-id, message-id, lease-id.",
		},
	},
	"send": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "sending session"},
			{Name: "kind", Positional: true, Required: true, Help: "message kind to enqueue"},
			{Name: "body-json", Help: "JSON body for the agent message; defaults to an empty object"},
		},
	},
	"block": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session reporting the blocker"},
			{Name: "job-id", Positional: true, Required: true, Help: "job being blocked"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "kind", Required: true, Help: "blocker kind matching ^[a-z0-9._-]{1,64}$"},
			{Name: "severity", Required: true, Values: []string{"blocked", "human_checkpoint"}, Help: "blocked keeps the run autonomous; human_checkpoint waits for the human principal"},
			{Name: "description", Required: true, Help: "plain-text blocker description, at most 8000 characters"},
		},
	},
	"complete": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session completing the job"},
			{Name: "job-id", Positional: true, Required: true, Help: "job to complete"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "summary", Help: "short completion summary recorded in the event payload"},
		},
	},
	"publish_artifact": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session publishing the artifact"},
			{Name: "job-id", Positional: true, Required: true, Help: "job that owns the artifact"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "kind", Positional: true, Required: true, Help: "artifact kind declared in expected_artifacts"},
			{Name: "logical-name", Positional: true, Required: true, Help: "logical artifact name declared in expected_artifacts"},
			{Name: "path", Positional: true, Required: true, Help: "repo-relative artifact path inside write_scope.allowed_paths"},
			{Name: "allow-no-process-execution", Bool: true, Help: "operator override for missing process execution evidence"},
			{Name: "override-rationale", Help: "required rationale for --allow-no-process-execution"},
		},
		Notes: []string{
			"Markdown artifacts with front matter must satisfy the kind's schema and any author line must exactly match expected_artifacts[].author_line.",
		},
	},
	"repo_write": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session performing the mediated repository write"},
			{Name: "job-id", Positional: true, Required: true, Help: "repo-write job that owns the target path"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "path", Positional: true, Required: true, Help: "repo-relative file path inside write_scope.allowed_paths"},
			{Name: "content", Required: true, Help: "exact UTF-8 file content to write atomically"},
		},
		Notes: []string{
			"repo.write refuses review-only scopes and paths outside write_scope.allowed_paths before writing.",
		},
	},
	"repo_patch": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session performing the mediated patch operation"},
			{Name: "job-id", Positional: true, Required: true, Help: "repo-write job that owns the target paths"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "patch", Required: true, Help: "unified git-style patch text"},
		},
		Notes: []string{
			"repo patch-preview checks applyability and write_scope without mutating files; repo patch-apply repeats the same checks before applying.",
		},
	},
	"process_run": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session performing the mediated command"},
			{Name: "job-id", Positional: true, Required: true, Help: "job that owns the active lease"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
			{Name: "command-json", Help: "JSON command array; arguments after -- are also accepted as the command array"},
			{Name: "timeout-seconds", Help: "maximum command runtime; defaults to 300 seconds and caps at 1800 seconds"},
			{Name: "process-id", Help: "stable process evidence id; generated when omitted"},
		},
		Notes: []string{
			"process.run requires capability_requirements.process_execution=true on the active job or a matching escape decision; it records process_executions evidence without storing stdout/stderr transcripts.",
		},
	},
	"verdict": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "review session recording the verdict"},
			{Name: "job-id", Positional: true, Required: true, Help: "review job"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the review job"},
			{Name: "verdict", Positional: true, Required: true, Values: []string{"accept", "accept_with_findings", "needs_revision", "reject"}, Help: "review verdict"},
			{Name: "findings-artifact-id", Help: "already-published findings artifact id to attach to the verdict"},
			{Name: "rationale", Help: "optional verdict rationale"},
			{Name: "review-provenance-decision-id", Help: "accepting run-level decision allowing unattested review provenance recovery"},
		},
		Notes: []string{
			"Use verdict instead of submit-review when the required finding artifact is already published for the current attempt.",
		},
	},
	"submit_review": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "review session publishing the finding"},
			{Name: "job-id", Positional: true, Required: true, Help: "review job"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the review job"},
			{Name: "path", Positional: true, Required: true, Help: "repo-relative finding path"},
			{Name: "verdict", Positional: true, Required: true, Values: []string{"accept", "accept_with_findings", "needs_revision", "reject"}, Help: "review verdict"},
			{Name: "logical-name", Help: "artifact logical name; inferred from the sole required expected artifact when possible, otherwise defaults to review"},
			{Name: "kind", Help: "artifact kind; inferred from the sole required expected artifact when possible, otherwise defaults to finding"},
			{Name: "rationale", Help: "optional verdict rationale"},
			{Name: "review-provenance-decision-id", Help: "accepting run-level decision allowing unattested review provenance recovery"},
		},
	},
	"worktree_create": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session that owns the job lease"},
			{Name: "job-id", Positional: true, Required: true, Help: "repo-write job requiring a per-job worktree"},
			{Name: "lease-id", Positional: true, Required: true, Help: "active lease for the job"},
		},
	},
	"worktree_release": {
		Params: []Param{
			{Name: "worktree-id", Positional: true, Required: true, Help: "worktree id returned by worktree create"},
			{Name: "force", Bool: true, Help: "remove even when the worktree HEAD is not reachable from the run branch or refs/striatum pins; records worktree.force_released"},
		},
	},
	"worktree_anchor": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "run that owns the completed job"},
			{Name: "job-id", Positional: true, Required: true, Help: "completed repo-write job whose worktree HEAD needs anchoring"},
			{Name: "worktree-id", Positional: true, Required: true, Help: "active or abandoned job worktree to anchor"},
		},
		Notes: []string{
			"Daemon-backed doctor remediation for completed jobs whose original lease is inactive: anchors the existing worktree HEAD through the same fast-forward-or-pin invariant used by work.complete and records worktree.anchored.",
		},
	},
	"worktree_gc": {
		Params: []Param{
			{Name: "run-id", Help: "limit garbage collection to a single run"},
			{Name: "sweep-pins", Bool: true, Help: "also sweep this run's refs/striatum pins (requires --run-id): delete pins whose tip is reachable from the integrated run/base branch, retain divergent pins, and report both (flag)"},
		},
		Notes: []string{
			"Removes only on-disk job worktrees whose jobs are terminal and whose HEAD is reachable from the run branch or refs/striatum pins; skipped rows are reported with reasons.",
			"--sweep-pins is an explicit run-closeout pin sweep: it deletes only integrated (reachable) pins, never deletes divergent pins, never runs git gc, and is idempotent — re-running reports no unexpected changes. Output lists pins_deleted, pins_retained, and reasons.",
		},
	},
	"repo_add": {
		Params: []Param{
			{Name: "path", Positional: true, Required: true, Help: "target repository path to register"},
			{Name: "init", Bool: true, Help: "create .striatum/scratch and add .striatum/ to .gitignore when registering a fresh target repo"},
			{Name: "display-name", Help: "operator-facing repository name; defaults to the directory basename"},
			{Name: "no-migrate", Bool: true, Help: "accepted compatibility flag; production registration never imports retired SQLite state"},
			{Name: "apply-blob-creation", Bool: true, Help: "create the blob-storage bucket for this repository if it does not yet exist; required when the daemon is configured for blob storage and the bucket is absent"},
			{Name: "blob-bucket", Help: "explicit blob-storage bucket name to provision (with --apply-blob-creation); defaults to the per-repo derived name"},
		},
	},
	"register_session": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "run that owns the session"},
			{Name: "role", Positional: true, Required: true, Help: "workflow role id (e.g. author, reviewer)"},
			{Name: "lane", Positional: true, Required: true, Help: "workflow lane id"},
			{Name: "capability", Repeatable: true, Help: "grant a capability; repeat per capability (defaults to the lane's declared capabilities). Note: the flag is --capability (singular), not --capabilities"},
			{Name: "fresh", Bool: true, Help: "register a fresh-context session"},
			{Name: "replace", Bool: true, Help: "atomically close any active session on this (run, role, lane) slot and transfer its leases before registering (alias: --force). Without it a duplicate active session is refused with the id to close, and parallel same-(role,lane) jobs each keep a distinct active session."},
			{Name: "force", Bool: true, Help: "alias for --replace"},
			{Name: "parent-session-id", Help: "parent session id for derived/continued context"},
			{Name: "operator-label", Help: "operator-visible label for the session"},
			{Name: "force-non-fresh", Bool: true, Help: "allow a non-fresh reviewer when the workflow declares reviewer_context_policy: fresh (requires --reason)"},
			{Name: "force-live", Bool: true, Help: "with --replace, permit displacing a session that is still heartbeating within the lease heartbeat window (otherwise refused with displaced_session_live). Requires --reason (#189)."},
			{Name: "reason", Help: "justification recorded with --force-non-fresh or --force-live"},
		},
		Notes: []string{
			"Aliases: `striatum session register ...` resolves to the same method (session.register).",
			"Parallel same-(role, lane) jobs (declared parallelism, disjoint scopes) each register their own fresh session and both stay active; the second registration no longer supersedes the first.",
			"--replace refuses to displace a session that heartbeated within the lease heartbeat window (it may be actively driving the same packet); pass --force-live --reason \"...\" to override and record why (#189).",
		},
	},
	"session_close": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session to close"},
			{Name: "reason", Required: true, Help: "why the session is being closed (the daemon rejects an empty reason: \"session close reason must not be empty\")"},
			{Name: "requeue-job", Bool: true, Help: "return this session's in-flight job to the queue on the same attempt (no attempt bump, no downstream reset) so a fresh lane can pick it up (#121)"},
		},
	},
	"checkpoint_resolve": {
		Params: []Param{
			{Name: "blocker-id", Positional: true, Required: true, Help: "human_checkpoint blocker to resolve"},
			{Name: "action", Positional: true, Required: true, Values: []string{"continue", "cancel", "override"}, Help: "continue re-runs the reviewer on the current branch (a revision cycle) — fix the findings first, or it re-reproduces the verdict and re-opens the checkpoint; cancel cancels the gated work; override is how you proceed past a revision_routing verdict, accepting it as superseded by a recorded decision (requires --decision-id)"},
			{Name: "decision-id", Help: "logical name of a run-level decision artifact to attach as the resolution rationale; required for override"},
		},
	},
	"decision_record": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "run the decision applies to"},
			{Name: "path", Positional: true, Required: true, Help: "repo-relative decision artifact path"},
			{Name: "outcome", Positional: true, Required: true, Values: []string{"accepted", "rejected", "accepted_with_follow_up"}, Help: "decision outcome"},
			{Name: "title", Positional: true, Required: true, Help: "human-readable decision title"},
			{Name: "decision-id", Help: "stable logical decision id; generated when omitted"},
			{Name: "rationale", Help: "decision rationale; required for escape decisions"},
			{Name: "follow-up", Help: "required follow-up text for accepted_with_follow_up"},
			{Name: "escape-surface", Help: "constrained-operator surface being escaped, e.g. shell_command or raw_file_write"},
			{Name: "escape-action", Help: "specific action authorized outside the constrained surface"},
			{Name: "subject-session-id", Help: "scope the decision to an exact session id (e.g. for a work.claim_override escape)"},
			{Name: "subject-job-id", Help: "scope the decision to an exact job id (e.g. for a work.claim_override escape)"},
			{Name: "mark-run-compromised", Bool: true, Help: "with an accepting decision, transition a completed run to compromised for provenance invalidation"},
		},
	},
	"supervise_start": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "session whose lane to supervise"},
			{Name: "replace", Bool: true, Help: "supersede any stale active supervisor for this session instead of refusing with a conflict error"},
			{Name: "provider-auth-gate", Values: []string{"auto", "required", "off"}, Help: "provider-auth preflight gate mode; default auto, required fails unsupported providers, off is the explicit rollback path"},
		},
	},
	"supervise_send": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "supervised session"},
			{Name: "packet-id", Positional: true, Required: true, Help: "work packet to deliver to the lane's stdin"},
		},
	},
	"supervise_stop": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "supervised session to stop"},
			{Name: "reason", Required: true, Help: "why the supervisor is being stopped (recorded as stop_reason)"},
		},
	},
	"supervise_status": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "supervised session to report on"},
		},
	},
	"supervise_trajectory": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "supervised session whose operator-local PTY log should be read"},
			{Name: "tail", Bool: true, Help: "print the last 200 lines instead of the whole log"},
			{Name: "tail-lines", Help: "print the last N lines instead of the whole log"},
		},
		Notes: []string{
			"Reads .striatum/scratch/<supervisor-id>/pty.log only; this is private operator scratch, not durable workflow provenance.",
		},
	},
	"supervise_list": {
		Params: []Param{
			{Name: "run-id", Positional: true, Required: true, Help: "run whose supervisors to list"},
			{Name: "state", Help: "filter by supervisor state (e.g. attached, stopped)"},
		},
	},
	"supervise_rebridge": {
		Params: []Param{
			{Name: "session-id", Positional: true, Required: true, Help: "supervised session to re-attach delivery for"},
		},
	},
}

// UsageFor returns the discoverability descriptor for a route's ParamsGroup.
// A hand-curated descriptor (which carries prose notes, optional-flag detail,
// and enum values) always wins; for every other group it synthesizes a
// generated descriptor from the params package's positional table so that no
// verb's `--help` falls back to the source-repo-only command-authority matrix
// (issue #194). The second return value reports whether a curated descriptor
// was found; the synthesized descriptor is still usable when it is false.
func UsageFor(group string) (Usage, bool) {
	if usage, ok := usageByGroup[group]; ok {
		return usage, true
	}
	return generatedUsageFor(group), false
}

// generatedUsageFor synthesizes a usage descriptor for an un-curated group from
// params.PositionalNames — the exact same table params.Build uses to map
// positional arguments — so the rendered positionals can never drift from real
// parsing behavior. Required-ness and optional flag names are NOT machine
// readable (the daemon handlers read them imperatively), so a generated
// descriptor lists positionals without asserting required-ness and points the
// operator at the daemon method for the rest.
func generatedUsageFor(group string) Usage {
	usage := Usage{Generated: true}
	for _, name := range params.PositionalNames(group) {
		usage.Params = append(usage.Params, Param{
			Name:       strings.ReplaceAll(name, "_", "-"),
			Positional: true,
		})
	}
	return usage
}

// localGroupSubcommands lists subcommands that are dispatched locally in
// cmd/striatum (before the daemon route table) and therefore have no Route entry,
// so the route-driven group help would otherwise omit them. #383 item 5: `run
// drive` is the canonical case — it exists and `run drive --help` works, but it
// was invisible in `run --help`, an asymmetry that hid the driver verb from the
// run family. Each entry is the human description shown in the group help in
// place of a daemon method (these verbs run a local loop, not a single RPC).
var localGroupSubcommands = map[string]map[string]string{
	"run": {
		"drive": "local operator loop: register + supervise lanes as queued jobs unblock (no daemon method)",
	},
}

// SubcommandsFor returns the sorted, non-deprecated subcommands registered for a
// command group (e.g. "recovery" -> ["accept-quarantined", "auto", ...]),
// including locally-dispatched subcommands (localGroupSubcommands) that have no
// Route entry. It returns nil when the command has no subcommand-bearing routes
// or local subcommands, so callers can distinguish a command group from a bare
// verb or an unknown command.
func SubcommandsFor(command string) []string {
	seen := map[string]bool{}
	subs := []string{}
	for _, route := range append(All(), runtimeRoutes...) {
		if route.Command != command || route.Deprecated || route.Subcommand == "" {
			continue
		}
		if seen[route.Subcommand] {
			continue
		}
		seen[route.Subcommand] = true
		subs = append(subs, route.Subcommand)
	}
	for sub := range localGroupSubcommands[command] {
		if seen[sub] {
			continue
		}
		seen[sub] = true
		subs = append(subs, sub)
	}
	sort.Strings(subs)
	return subs
}

// RenderCommandGroupHelp formats the help for a command group that carries only
// subcommands (no bare verb) — e.g. `striatum recovery --help`. Before this,
// `recovery` and `recovery --help` both fell through to the daemon route as
// "unknown command: recovery" (#389 gap 3), leaving the recovery verb family
// undiscoverable from the CLI. It lists each subcommand with its daemon method
// and required capability, and points at per-subcommand `--help` for flags.
// Returns "" when the command has no subcommands (caller falls back to the
// unknown-command path).
func RenderCommandGroupHelp(command string) string {
	subs := SubcommandsFor(command)
	if len(subs) == 0 {
		return ""
	}
	methodBySub := map[string]Route{}
	for _, route := range append(All(), runtimeRoutes...) {
		if route.Command == command && route.Subcommand != "" && !route.Deprecated {
			if _, ok := methodBySub[route.Subcommand]; !ok {
				methodBySub[route.Subcommand] = route
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "usage: striatum %s <subcommand> [flags]\n\n", command)
	fmt.Fprintf(&b, "Subcommands (run `striatum %s <subcommand> --help` for a subcommand's flags):\n", command)
	for _, sub := range subs {
		route, isRoute := methodBySub[sub]
		var line string
		switch {
		case isRoute:
			line = fmt.Sprintf("  %-22s %s", sub, route.Method)
			if route.RequiredCapability != "" {
				line += "  (capability: " + route.RequiredCapability + ")"
			}
		default:
			// Locally-dispatched subcommand (no daemon method) — render its
			// description so the verb is discoverable in the group help.
			line = fmt.Sprintf("  %-22s %s", sub, localGroupSubcommands[command][sub])
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// IsHelpArg reports whether arg requests help for the verb.
func IsHelpArg(arg string) bool {
	switch arg {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

// RenderHelp formats a verb's usage. command is the resolved invocation prefix
// (e.g. "supervise start" or "register-session").
func (r Route) RenderHelp() string {
	command := r.Command
	if r.Subcommand != "" {
		command += " " + r.Subcommand
	}
	// UsageFor always returns a usable descriptor: curated when registered,
	// otherwise synthesized from the params positional table (#194).
	usage, _ := UsageFor(r.ParamsGroup)

	var b strings.Builder
	synopsis := "usage: striatum " + command
	for _, p := range usage.Params {
		synopsis += " " + p.synopsisToken()
	}
	b.WriteString(synopsis)
	b.WriteString("\n")

	fmt.Fprintf(&b, "method: %s", r.Method)
	if r.RequiredCapability != "" {
		fmt.Fprintf(&b, "  (capability: %s)", r.RequiredCapability)
	}
	b.WriteString("\n")

	if usage.Generated {
		return b.String() + renderGeneratedBody(usage.Params)
	}

	required := []Param{}
	optional := []Param{}
	for _, p := range usage.Params {
		if p.Required {
			required = append(required, p)
		} else {
			optional = append(optional, p)
		}
	}
	if len(required) > 0 {
		b.WriteString("\nrequired:\n")
		for _, p := range required {
			b.WriteString(p.helpLine())
		}
	}
	if len(optional) > 0 {
		b.WriteString("\noptional:\n")
		for _, p := range optional {
			b.WriteString(p.helpLine())
		}
	}
	for _, note := range usage.Notes {
		b.WriteString("\n")
		b.WriteString(note)
		b.WriteString("\n")
	}
	return b.String()
}

// renderGeneratedBody renders the body for a generated (un-curated) verb. It
// lists the positional arguments straight from the params table and points the
// operator at locally-resolvable help rather than the source-repo-only
// command-authority matrix (issue #194). For flag-only / no-argument verbs
// (params has no positionals) it explains that the inputs are flags derived
// from the daemon method and how to reach them without the source repo.
func renderGeneratedBody(positionals []Param) string {
	var b strings.Builder
	if len(positionals) > 0 {
		b.WriteString("\npositional arguments:\n")
		for _, p := range positionals {
			b.WriteString(p.helpLine())
		}
		b.WriteString("\nOptional flags and required-ness are derived from the daemon method; this\n")
		b.WriteString("verb does not have curated flag help yet. Run `striatum --help` for the\n")
		b.WriteString("verb index, and the daemon rejects a call that is missing a required input\n")
		b.WriteString("with a `<method> requires <param>` error that names the missing flag.\n")
		return b.String()
	}
	b.WriteString("\nThis verb takes flags derived from the daemon method (no positional\n")
	b.WriteString("arguments). Run `striatum --help` for the verb index; the daemon rejects a\n")
	b.WriteString("call that is missing a required input with a `<method> requires <param>`\n")
	b.WriteString("error that names the missing flag.\n")
	return b.String()
}

func (p Param) synopsisToken() string {
	if p.Positional {
		if p.Required {
			return "<" + p.Name + ">"
		}
		return "[" + p.Name + "]"
	}
	token := "--" + p.Name
	switch {
	case p.Bool:
		// presence flag: no value token
	case len(p.Values) > 0:
		token += " " + strings.Join(p.Values, "|")
	default:
		token += " <value>"
	}
	if p.Repeatable {
		token += " ..."
	}
	if p.Required {
		return token
	}
	return "[" + token + "]"
}

func (p Param) helpLine() string {
	name := "--" + p.Name
	if p.Positional {
		name = "<" + p.Name + "> | " + name
	}
	suffix := ""
	if p.Bool {
		suffix += " (flag)"
	}
	if p.Repeatable {
		suffix += " (repeatable)"
	}
	if len(p.Values) > 0 {
		suffix += " {" + strings.Join(p.Values, "|") + "}"
	}
	return fmt.Sprintf("  %-26s %s%s\n", name, p.Help, suffix)
}

// HelpGroups returns the sorted set of ParamsGroups with registered usage; it
// keeps tests stable and lets tooling enumerate documented verbs.
func HelpGroups() []string {
	groups := make([]string, 0, len(usageByGroup))
	for group := range usageByGroup {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	return groups
}
