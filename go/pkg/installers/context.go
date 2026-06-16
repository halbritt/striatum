package installers

// Template context for skill/plugin rendering. Ported verbatim from
// src/striatum/skills/context.py so the rendered bundles are byte-equivalent
// to the Python installer for a given runner version. Order is presentation
// order in the rendered Markdown; do not sort the verb groups alphabetically.

type verbEntry struct {
	Verb    string
	Summary string
}

// verbGroups mirrors VERB_TABLE. Slices preserve the curated insertion order
// that Python dicts guaranteed.
var verbGroups = map[string][]verbEntry{
	"scaffold": {
		{"repo add", "Register the target repo with the daemon (live state lives in the daemon's PostgreSQL under a `repository_id` scope per D094 / RFC 0043)."},
		{"workflow generate", "Render a starter `workflow.json` plus role/prompt stubs from a catalog shape."},
		{"workflow validate", "Validate a workflow file; non-zero exit on error."},
		{"run prepare", "Snapshot the workflow and prepare a run id."},
		{"run start", "Transition a prepared run to `running` and enqueue eligible root jobs for registered sessions to claim."},
		{"branch confirm", "Confirm or create the run's working branch."},
	},
	"claim_loop": {
		{"register-session", "Open an agent session for a (role, lane) pair."},
		{"claim-next", "Claim the next ready work packet for a session."},
		{"ack", "Acknowledge a claimed packet and start work."},
		{"heartbeat", "Extend the active lease while work continues."},
		{"publish-artifact", "Publish a build artifact for a job."},
		{"verdict", "Record a non-finding verdict for a review job."},
		{"submit-review", "Publish a finding artifact + verdict in one call."},
		{"complete", "Mark a non-review job complete."},
		{"worktree create", "Create the per-job git worktree (RFC 0008)."},
		{"worktree release", "Release a per-job worktree when work is done."},
	},
	"supervise": {
		{"supervise start", "Start a long-lived agent CLI for the session."},
		{"supervise send", "Deliver a packet to a supervisor's stdin pipe."},
		{"supervise stop", "Stop a supervisor (SIGTERM, SIGKILL after 5s)."},
		{"supervise status", "Probe a supervisor's liveness."},
		{"supervise list", "List supervisors for a run."},
	},
	"recover": {
		{"status", "Read run/job/blocker state."},
		{"why", "Explain why a job is in its current state."},
		{"doctor --verbose", "Surface structured consistency problems."},
		{"recovery stale-leases", "Lazy-expire and report stale leases."},
		{"recovery requeue-stale", "Requeue an expired non-repo-write job."},
		{"recovery process-reconcile", "Reconcile externally-killed processes."},
		{"recovery resume", "Resolve remediated process-adapter blockers."},
		{"recovery complete-stalled", "Finalize a dead lane's job from its durable artifacts instead of hand-capturing the worktree (D200)."},
		{"recovery resolve-blocker", "Close a dangling non-escalation, non-checkpoint blocker that no completion path cleared (#304)."},
		{"checkpoint resolve", "Resolve a `human_checkpoint` blocker."},
		{"dashboard --once", "Render a single dashboard frame for scripts."},
	},
}

// boundaries mirrors BOUNDARIES — single-sentence statements reused across skills.
var boundaries = []string{
	"Do not bypass the daemon (the CLI is the only client; the daemon is the single writer per D094 / RFC 0043); never open Postgres directly, and do not treat `.striatum/` scratch files as workflow state.",
	"Do not treat marker files, tmux panes, or terminal output as workflow state.",
	"Do not advance state by printing phrases; advance by calling the CLI verbs in your work packet.",
	"Do not capture stdout/stderr transcripts; D028 keeps them off by default.",
	"Do not derive bylines from job titles; use the byline supplied in your work packet.",
	"Do not parse a supervisor's own output for workflow state; supervisors send DEVNULL.",
	"Do not paste over a broken runner: never hand-finish stranded or wedged work (manual worktree capture, cherry-pick, or hand-commit) and report it complete, and never proceed while `doctor` is red — recover through the daemon (`recovery requeue-stale`/`resume`/`complete-stalled`, `checkpoint resolve`) or surface the defect (file an issue, record the friction, escalate) instead of masking it as a success.",
}

// frontMatterKinds mirrors sorted(ALLOWED_ARTIFACT_KINDS) from
// src/striatum/artifact_contracts.py. Kept sorted to match the Python output.
var frontMatterKinds = []string{
	"action_item_ledger",
	"auto_finalize_gate_evidence",
	"commit_request",
	"decision",
	"escalation",
	"finding",
	"findings_ledger",
	"handoff",
	"harness_improvement_proposal",
	"marker",
	"operator_brief",
	"operator_report",
	"other",
	"patch_summary",
	"pr_request",
	"progress_note",
	"prompt",
	"support_ledger",
	"synthesis",
	"test_report",
	"work_plan",
}

// frontMatterSkeletonKinds are the front-matter-bearing artifact kinds most
// commonly authored inside agent work loops. The full canonical list remains
// docs/reference/spec.md#artifact-front-matter-schemas; these skeletons keep a
// consumer-repo agent from having to inspect Striatum source for routine
// finding/ledger/review artifacts (#159).
var frontMatterSkeletonKinds = []string{
	"finding",
	"findings_ledger",
	"synthesis",
	"support_ledger",
	"action_item_ledger",
	"collaboration_ledger",
	"escalation",
	"harness_improvement_proposal",
}
