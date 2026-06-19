package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/verifier"
	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
)

func HandleRunPrepare(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	workflowPath := stringParam(envelope, "workflow")
	if workflowPath == "" {
		return nil, rpc.NewError("schema_invalid", "run.prepare requires workflow", nil)
	}
	// #355 victim-side mitigation: run.prepare is a plain control-plane append
	// (`run.created`) that does nothing slow, yet under a multi-run supervise storm
	// it was starved 100% behind a lock-holding sweep/reconcile transaction and
	// surfaced the raw `append_event_row (sd): 57014` (it had no transient-load
	// swallow). Wrap it in a bounded retry on isTransientDaemonLoadError so back-
	// pressure self-heals instead of hard-failing the operator. The primary #355
	// fix removes the convoy's source; this bounds the blast radius.
	return withTxRetryOnTransientLoad(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		return runPrepare(ctx, tx, repositoryID, workflowPath)
	})
}

func HandleRunStart(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.start requires run_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0108 Phase 2: take the per-repo advisory lock FIRST (before the
		// run-scoped FOR UPDATE below) so concurrent run.starts on this repo
		// serialize. The isolation precondition reads "is a sibling run already
		// active?" then mutates run state; without this lock two starts could both
		// see no sibling and race two runs onto the shared main checkout.
		if err := lockRepo(ctx, tx, repositoryID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		workflow, err := workflowForRun(ctx, tx, repositoryID, run)
		if err != nil {
			return nil, err
		}
		if workflow["provenance_mode"] == "sealed_patch" {
			return nil, rpc.NewError("workflow_error", "provenance_mode sealed_patch is unsupported: no containment mechanism shipped; sealed runs refuse to start rather than silently downgrading to advisory", nil)
		}
		state := fmt.Sprint(run["state"])
		if state == "needs_branch_confirmation" {
			return nil, rpc.NewError("workflow_error", "branch confirmation is required before run start", nil)
		}
		if state != "ready" && state != "running" {
			return nil, rpc.NewError("invalid_transition", "run cannot be started from its current state", nil)
		}
		var warnings []string
		if state == "ready" {
			// RFC 0141 Pillar 3 (UNFILLED): refuse to start a verification gate whose
			// external checks are sanctioned-but-unpinned on this host — it would mint
			// no evidence and report a false green. This is a PURE FILE READ of the
			// committed intent + per-host pins (D227: the daemon validates sealed
			// bytes, it executes nothing); the block clears once the operator pins.
			if tb := verifier.EvaluateAllowlistTemplate(fmt.Sprint(run["repo_root"]), workflow); tb != nil {
				return nil, rpc.NewError(tb.Reason, tb.Message, map[string]any{
					"fix_command":      tb.FixCommand,
					"unpinned_entries": tb.UnpinnedEntries,
					"intent_path":      tb.IntentPath,
				})
			}
			// #242: supervised/agent-loop repo-write lanes must never launch into
			// the shared checkout unless the workflow records the explicit
			// interactive-human compatibility override. This is independent of
			// sibling concurrency: autonomous lanes can collide with the operator's
			// own checkout even when they are the only active run.
			if err := enforceAutonomousRepoWriteIsolation(ctx, tx, repositoryID, runID, workflow); err != nil {
				return nil, err
			}
			// RFC 0108 Phase 2 — isolation by default under concurrency. Refuse to
			// start a run that would write the SHARED main checkout while another run
			// is already active on this repo. Checked BEFORE the state mutation so a
			// refused start leaves the run `ready` (the operator fixes isolation, or
			// waits for the active run, then retries).
			if err := enforceConcurrentRunIsolation(ctx, tx, repositoryID, runID, workflow); err != nil {
				return nil, err
			}
			// RFC 0108 Phase 3 — cross-run collision detection. A same-branch
			// collision is refused (cross_run_collision) unless --allow-overlap; an
			// overlapping write_scope is surfaced as a non-blocking warning.
			collisionWarnings, err := evaluateCrossRunCollision(ctx, tx, repositoryID, runID, fmt.Sprint(run["branch_name"]), boolParam(envelope, "allow_overlap"))
			if err != nil {
				return nil, err
			}
			warnings = collisionWarnings
			now := nowString()
			if err := tx.Exec(ctx, `
				UPDATE striatumd.runs
				   SET state = 'running', started_at = $1
				 WHERE repository_id = $2 AND run_id = $3`, now, repositoryID, runID); err != nil {
				return nil, err
			}
			roots, err := queryRows(ctx, tx, `
				SELECT j.job_id
				  FROM striatumd.jobs j
				 WHERE j.repository_id = $1
				   AND j.run_id = $2
				   AND NOT EXISTS (
				     SELECT 1 FROM striatumd.job_dependencies dep
				      WHERE dep.repository_id = j.repository_id
				        AND dep.job_id = j.job_id
				   )
				 ORDER BY j.created_at`, repositoryID, runID)
			if err != nil {
				return nil, err
			}
			for _, root := range roots {
				if _, err := enqueueJob(ctx, tx, repositoryID, fmt.Sprint(root["job_id"])); err != nil {
					return nil, err
				}
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.started", nil, nil, nil, nil, nil, nil); err != nil {
				return nil, err
			}
			// RFC 0122: when the snapshot opts any lane into supervision.auto_spawn,
			// capture the run-owner's pre-authorization grant atomically with the run
			// becoming `running`. The daemon scheduler later REPLAYS this grant to
			// spawn lanes with no operator RPC — it never invents authority. Resolving
			// run-as here (the operator's live request) and refusing loudly on an
			// unresolved identity is the gate RFC 0122 §4 requires.
			if workflowHasAutoSpawnLane(workflow) {
				ownerPrincipalID := db.AuthorityFromContext(ctx).PrincipalID
				if err := captureSpawnAuthorizationGrant(ctx, tx, repositoryID, runID, ownerPrincipalID); err != nil {
					return nil, err
				}
			}
		}
		result := map[string]any{"run_id": runID, "state": "running"}
		if len(warnings) > 0 {
			result["warnings"] = warnings
		}
		return result, nil
	})
}

// enforceConcurrentRunIsolation is the RFC 0108 Phase 2 precondition: once a run
// is active on a repository, a SECOND run that would write the SHARED main
// checkout — a repo-write job on a lane without worktree_isolation: per_job — is
// refused at start, so no two concurrent runs ever scribble one working tree.
// This promotes the `repo_write_without_worktree_isolation` lint warning to an
// enforced precondition under concurrency. A run whose repo-write work is
// per_job-isolated (its own detached worktree) or that does not write the repo
// starts freely beside the active sibling; the single-run case (no sibling
// active) is untouched. Must run inside a transaction holding lockRepo so the
// sibling-active check cannot race two starts onto the shared checkout.
func enforceConcurrentRunIsolation(ctx context.Context, tx db.TxRunner, repositoryID, runID string, workflow map[string]any) error {
	sibling, err := otherActiveRunOnRepo(ctx, tx, repositoryID, runID)
	if err != nil {
		return err
	}
	if sibling == "" {
		return nil
	}
	offender, err := firstUnisolatedRepoWriteJob(ctx, tx, repositoryID, runID, workflow)
	if err != nil {
		return err
	}
	if offender == "" {
		return nil
	}
	return rpc.NewError("concurrent_run_isolation_required", fmt.Sprintf(
		"run %s is already active on this repository, and job %q does repo-write work on a lane without worktree_isolation: per_job — starting this run would share the main checkout with the active run. Set worktree_isolation: per_job on the repo-write lane (each run then gets its own detached worktree), or wait for the active run to finish.",
		sibling, offender), nil)
}

func enforceAutonomousRepoWriteIsolation(ctx context.Context, runner any, repositoryID, runID string, workflow map[string]any) error {
	jobID, laneID, err := firstAutonomousSharedCheckoutRepoWriteJob(ctx, runner, repositoryID, runID, workflow)
	if err != nil {
		return err
	}
	if jobID == "" {
		return nil
	}
	return rpc.NewError("autonomous_worktree_isolation_required",
		workflowauthoring.AutonomousWorktreeIsolationRefusalMessage(jobID, laneID), nil)
}

func firstAutonomousSharedCheckoutRepoWriteJob(ctx context.Context, runner any, repositoryID, runID string, workflow map[string]any) (string, string, error) {
	jobs, err := queryRows(ctx, runner, `
		SELECT job_id, write_scope_json, lane_selector_json
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY created_at, job_id`, repositoryID, runID)
	if err != nil {
		return "", "", err
	}
	lanes := asMap(workflow["lanes"])
	for _, job := range jobs {
		if !isRepoWrite(job) {
			continue
		}
		laneID := jobLaneID(job)
		if laneID == "" {
			continue
		}
		laneRaw, exists := lanes[laneID]
		if !exists {
			continue
		}
		lane := asMap(laneRaw)
		if workflowauthoring.LaneRequiresWorktreeIsolationForAutonomousRepoWrite(lane) {
			return fmt.Sprint(job["job_id"]), laneID, nil
		}
	}
	return "", "", nil
}

// otherActiveRunOnRepo returns the id of one OTHER run on the repo that is
// currently `running` (the state in which jobs are claimed and the working tree
// is touched), or "" when this is the only active run.
func otherActiveRunOnRepo(ctx context.Context, runner any, repositoryID, runID string) (string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT run_id
		  FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id <> $2 AND state = 'running'
		 ORDER BY started_at NULLS LAST, run_id
		 LIMIT 1`, repositoryID, runID)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return fmt.Sprint(rows[0]["run_id"]), nil
}

// firstUnisolatedRepoWriteJob returns the id of the first job in the run that
// performs repo-write work on a lane WITHOUT worktree_isolation: per_job — a job
// that would run in the shared main checkout. "" when every repo-write job is
// per_job-isolated (or there is none). The isolation decision mirrors buildPacket
// / HandleWorktreeCreate exactly: laneWorktreeIsolation over the run's frozen
// workflow snapshot, isRepoWrite over the job's stored write_scope.
func firstUnisolatedRepoWriteJob(ctx context.Context, runner any, repositoryID, runID string, workflow map[string]any) (string, error) {
	jobs, err := queryRows(ctx, runner, `
		SELECT job_id, write_scope_json, lane_selector_json
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2
		 ORDER BY created_at, job_id`, repositoryID, runID)
	if err != nil {
		return "", err
	}
	for _, job := range jobs {
		if !isRepoWrite(job) {
			continue
		}
		if laneWorktreeIsolation(workflow, jobLaneID(job)) != "per_job" {
			return fmt.Sprint(job["job_id"]), nil
		}
	}
	return "", nil
}

// evaluateCrossRunCollision is the RFC 0108 Phase 3 cross-run collision check at
// run.start. It distinguishes a DEFINITE collision from a POTENTIAL one:
//
//   - Same target branch: two runs cannot share one git branch — they would
//     clobber each other and collide at integration — so the start is REFUSED
//     with cross_run_collision (unless the operator passes allow_overlap).
//   - Overlapping repo-write allowed_paths: on distinct branches + per_job
//     worktrees the two runs do not collide at write time, but their changes will
//     likely conflict at integration (the VCS merge problem P4 serializes). That
//     is surfaced as a NON-BLOCKING warning (the RFC 0102 attention principle) so
//     the operator sees it up front rather than discovering it at merge.
//
// allow_overlap suppresses both the refusal and the warnings — the operator has
// explicitly accepted the overlap. Must run inside the lockRepo-held run.start
// transaction so the active-runs snapshot cannot race a concurrent start.
func evaluateCrossRunCollision(ctx context.Context, tx db.TxRunner, repositoryID, runID, branch string, allowOverlap bool) ([]string, error) {
	if allowOverlap {
		return nil, nil
	}
	if branch != "" && branch != "<nil>" {
		other, err := otherActiveRunOnBranch(ctx, tx, repositoryID, runID, branch)
		if err != nil {
			return nil, err
		}
		if other != "" {
			return nil, rpc.NewError("cross_run_collision", fmt.Sprintf(
				"run %s is already active on branch %q — two runs cannot target the same branch concurrently (they would collide at integration). Give this run a distinct branch, or pass --allow-overlap to start anyway.",
				other, branch), nil)
		}
	}
	thisAllowed, err := runRepoWriteAllowedPaths(ctx, tx, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	return crossRunWriteScopeWarnings(ctx, tx, repositoryID, runID, thisAllowed)
}

// otherActiveRunOnBranch returns the id of one OTHER `running` run on the repo
// that targets the same branch, or "" when none does.
func otherActiveRunOnBranch(ctx context.Context, runner any, repositoryID, runID, branch string) (string, error) {
	rows, err := queryRows(ctx, runner, `
		SELECT run_id
		  FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id <> $2 AND state = 'running' AND branch_name = $3
		 ORDER BY started_at NULLS LAST, run_id
		 LIMIT 1`, repositoryID, runID, branch)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", nil
	}
	return fmt.Sprint(rows[0]["run_id"]), nil
}

// runRepoWriteAllowedPaths returns the deduped, normalized union of allowed_paths
// across a run's repo-write jobs (the paths this run intends to write).
func runRepoWriteAllowedPaths(ctx context.Context, runner any, repositoryID, runID string) ([]string, error) {
	jobs, err := queryRows(ctx, runner, `
		SELECT write_scope_json
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for _, job := range jobs {
		if !isRepoWrite(job) {
			continue
		}
		for _, p := range stringListFromAny(asMap(job["write_scope_json"])["allowed_paths"]) {
			if clean, ok := normalizeScopePath(p); ok {
				paths = append(paths, clean)
			}
		}
	}
	return dedupeStrings(paths), nil
}

// crossRunWriteScopeWarnings returns one warning per OTHER active run whose
// repo-write allowed_paths prefix-overlap this run's. Empty when this run writes
// nothing or no active sibling's write scope overlaps.
func crossRunWriteScopeWarnings(ctx context.Context, runner any, repositoryID, runID string, thisAllowed []string) ([]string, error) {
	if len(thisAllowed) == 0 {
		return nil, nil
	}
	rows, err := queryRows(ctx, runner, `
		SELECT r.run_id AS run_id, j.write_scope_json AS write_scope_json
		  FROM striatumd.runs r
		  JOIN striatumd.jobs j
		    ON j.repository_id = r.repository_id AND j.run_id = r.run_id
		 WHERE r.repository_id = $1 AND r.run_id <> $2 AND r.state = 'running'`,
		repositoryID, runID)
	if err != nil {
		return nil, err
	}
	byRun := map[string][]string{}
	for _, row := range rows {
		if !isRepoWrite(row) {
			continue
		}
		other := fmt.Sprint(row["run_id"])
		for _, p := range stringListFromAny(asMap(row["write_scope_json"])["allowed_paths"]) {
			if clean, ok := normalizeScopePath(p); ok {
				byRun[other] = append(byRun[other], clean)
			}
		}
	}
	warnings := make([]string, 0)
	for other, paths := range byRun {
		if overlap := firstPathOverlap(thisAllowed, dedupeStrings(paths)); overlap != "" {
			warnings = append(warnings, fmt.Sprintf(
				"write_scope overlaps active run %s at path %q — expect a merge conflict at integration; pass --allow-overlap to silence (RFC 0108 Phase 3)",
				other, overlap))
		}
	}
	return dedupeStrings(warnings), nil
}

// firstPathOverlap returns the first path in `a` that prefix-overlaps any path in
// `b` (a is a prefix of b, b is a prefix of a, equal, or either is the repo root
// "."), or "" when the two scope sets are disjoint.
func firstPathOverlap(a, b []string) string {
	for _, pa := range a {
		for _, pb := range b {
			if pathPrefixOverlap(pa, pb) {
				return pa
			}
		}
	}
	return ""
}

// pathPrefixOverlap reports whether two normalized scope paths overlap: either is
// the whole repo ("."), they are equal, or one is a path-prefix of the other.
func pathPrefixOverlap(a, b string) bool {
	if a == "." || b == "." {
		return true
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func HandleRunPause(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.pause requires run_id", nil)
	}
	reason := stringParam(envelope, "reason")
	if reason == "" {
		reason = "operator_paused"
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		state := fmt.Sprint(run["state"])
		if state == "completed" || state == "failed" || state == "canceled" || state == "compromised" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is in terminal state %q and cannot be paused", state), nil)
		}
		if nullable(run["paused_at"]) != nil {
			return map[string]any{"run_id": runID, "state": state, "paused_at": run["paused_at"], "status": "already_paused"}, nil
		}
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.runs
			   SET paused_at = $1, paused_reason = $2
			 WHERE repository_id = $3 AND run_id = $4 AND paused_at IS NULL`, now, reason, repositoryID, runID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.paused", nil, nil, nil, nil, nil, map[string]any{"reason": reason}); err != nil {
			return nil, err
		}
		return map[string]any{"run_id": runID, "state": state, "paused_at": now, "status": "paused"}, nil
	})
}

func HandleRunResume(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.resume requires run_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		state := fmt.Sprint(run["state"])
		if state == "completed" || state == "compromised" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is in terminal state %q; completed/compromised runs cannot be resumed. To correct compromised provenance, record a human decision with --mark-run-compromised and start a replacement run.", state), nil)
		}
		if state == "failed" || state == "canceled" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is in terminal state %q; use retry_job to revive failed or canceled work", state), nil)
		}
		if nullable(run["paused_at"]) == nil {
			return map[string]any{"run_id": runID, "state": state, "paused_at": nil, "status": "not_paused"}, nil
		}
		if err := tx.Exec(ctx, `
			UPDATE striatumd.runs
			   SET paused_at = NULL, paused_reason = NULL
			 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID); err != nil {
			return nil, err
		}
		// #383 item 1: clearing paused_at is necessary but not sufficient to make
		// the run progress again. The driving loop is one of two homes: the daemon
		// auto_spawn scheduler (RFC 0122, grant-gated + deployment opt-in) or the
		// operator-side `run drive` loop. Resume itself starts neither — it only
		// lifts the C5 pause hold. Classify which home will (or must) re-drive the
		// resumed run and report it loudly so a resumed-but-undriven run is never a
		// silent stall. The classification is read-only and adds no new RPC.
		drive := resolveResumeDrivePlan(ctx, tx, repositoryID, runID)
		payload := map[string]any{
			"drive":         drive.Mode,
			"drive_message": drive.Message,
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.resumed", nil, nil, nil, nil, nil, payload); err != nil {
			return nil, err
		}
		result := map[string]any{
			"run_id":        runID,
			"state":         state,
			"paused_at":     nil,
			"status":        "resumed",
			"drive":         drive.Mode,
			"drive_message": drive.Message,
		}
		if drive.NextAction != "" {
			result["next_action"] = drive.NextAction
		}
		return result, nil
	})
}

// resumeDrivePlan describes how a just-resumed run will (or must) be re-driven.
// It is the legibility half of the #383 item 1 fix: resume can re-arm the daemon
// auto_spawn scheduler implicitly (the scheduler re-adopts every running,
// non-paused run with an active grant on its next sweep), but it cannot recreate
// the operator-side transient `run drive` loop — that lives in a systemd unit the
// daemon does not own. So resume SAYS which path applies instead of returning a
// bare {status:resumed} that hides an undriven run.
type resumeDrivePlan struct {
	// Mode is the machine-readable drive home: "daemon_auto_spawn" (the scheduler
	// will re-adopt the run with no operator RPC), "operator_run_drive" (the
	// operator must re-invoke `run drive`), or "auto_spawn_scheduler_disabled" (the
	// run is grant-backed but the daemon scheduler is opt-OFF on this deployment,
	// so `run drive` is still required until it is enabled).
	Mode string
	// Message is a one-line human explanation of Mode.
	Message string
	// NextAction is a copy-pasteable operator command when one is required, "" when
	// the daemon will re-drive on its own.
	NextAction string
}

// resolveResumeDrivePlan classifies the post-resume drive home for a run. It is
// read-only: it inspects the run's active spawn-authorization grant and the
// daemon's STRIATUM_AUTO_SPAWN_SCHEDULER deployment flag, and never spawns or
// mutates anything. A grant exists only when the run's workflow opted a lane into
// supervision.auto_spawn at run.start (captureSpawnAuthorizationGrant), so its
// presence is the same signal the AutoSpawnSweep candidate query keys on.
func resolveResumeDrivePlan(ctx context.Context, runner any, repositoryID, runID string) resumeDrivePlan {
	driveHint := fmt.Sprintf("re-invoke the driver: striatum run drive --run-id %s", runID)
	grant, err := loadActiveSpawnGrant(ctx, runner, repositoryID, runID)
	if err != nil || grant == nil || grant.Expired {
		// No live grant (or it could not be read / has expired): the auto_spawn
		// scheduler will not adopt this run, so the operator-side `run drive` loop is
		// the only driver. The transient driver started by `run start` does not
		// survive a daemon/DB outage, so after a pause+restart it must be re-invoked.
		return resumeDrivePlan{
			Mode:       "operator_run_drive",
			Message:    "run resumed; it has no active auto_spawn grant, so the daemon scheduler will not re-drive it. Resume only lifts the pause hold — re-invoke `run drive` to restart the driving loop (the transient driver from `run start` does not survive a daemon/DB restart).",
			NextAction: driveHint,
		}
	}
	if !autoSpawnSchedulerEnabled() {
		// The run is grant-backed, but this deployment runs the daemon scheduler
		// opt-OFF (STRIATUM_AUTO_SPAWN_SCHEDULER unset). Until it is enabled the
		// operator-side `run drive` is still the driver.
		return resumeDrivePlan{
			Mode:       "auto_spawn_scheduler_disabled",
			Message:    "run resumed; it holds an active auto_spawn grant, but the daemon auto_spawn scheduler is disabled on this deployment (STRIATUM_AUTO_SPAWN_SCHEDULER unset). Re-invoke `run drive` to restart the driving loop, or enable the scheduler to have the daemon re-adopt resumed runs automatically.",
			NextAction: driveHint,
		}
	}
	return resumeDrivePlan{
		Mode:       "daemon_auto_spawn",
		Message:    "run resumed; it holds an active auto_spawn grant and the daemon scheduler is enabled, so the scheduler will re-adopt and re-drive it on its next sweep with no operator RPC. No manual `run drive` is required.",
		NextAction: "",
	}
}

// autoSpawnSchedulerEnabled reports whether this daemon deployment runs the
// RFC 0122 auto_spawn scheduler (STRIATUM_AUTO_SPAWN_SCHEDULER truthy). It mirrors
// the envBool gate cmd/striatumd uses to decide whether to start the sweep, so the
// resume drive classification reflects the live deployment rather than guessing.
func autoSpawnSchedulerEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("STRIATUM_AUTO_SPAWN_SCHEDULER"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isTerminalRunState(state string) bool {
	switch state {
	case "completed", "failed", "canceled", "compromised":
		return true
	default:
		return false
	}
}

func releaseActiveRunLeases(ctx context.Context, runner any, repositoryID, runID, now, reason string) error {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	return exec.Exec(ctx, `
		UPDATE striatumd.leases
		   SET state = 'released', released_at = $1, release_reason = $2
		 WHERE repository_id = $3
		   AND run_id = $4
		   AND state = 'active'`, now, reason, repositoryID, runID)
}

func cancelRunInTx(ctx context.Context, tx db.TxRunner, repositoryID, runID, reason string) (map[string]any, error) {
	run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
	if err != nil {
		return nil, err
	}
	state := fmt.Sprint(run["state"])
	if state == "canceled" {
		now := nowString()
		if err := releaseActiveRunLeases(ctx, tx, repositoryID, runID, now, "run_canceled"); err != nil {
			return nil, err
		}
		if err := closeRemainingSessions(ctx, tx, repositoryID, runID, "run_canceled", "run_canceled"); err != nil {
			return nil, err
		}
		return map[string]any{"run_id": runID, "state": "canceled", "status": "already_canceled"}, nil
	}
	if isTerminalRunState(state) {
		return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is in terminal state %q and cannot be canceled", state), nil)
	}
	now := nowString()
	if err := tx.Exec(ctx, `
		UPDATE striatumd.jobs
		   SET state = 'canceled', completed_at = $1
		 WHERE repository_id = $2
		   AND run_id = $3
		   AND state IN ('blocked','queued','claimed','running','stale_lease','waiting_human')`, now, repositoryID, runID); err != nil {
		return nil, err
	}
	if err := releaseActiveRunLeases(ctx, tx, repositoryID, runID, now, "run_canceled"); err != nil {
		return nil, err
	}
	// RFC 0118 P1-5: freeze the completion record while the sessions are still
	// live; cancellation is a terminal run-finalization path regardless of
	// whether the operator or recovery sweep initiated it.
	cancelPayload, err := freezeRunCompletionRecord(ctx, tx, repositoryID, runID, "canceled", "run_canceled",
		map[string]any{"stop_reason": reason}, map[string]any{"reason": reason})
	if err != nil {
		return nil, err
	}
	if err := tx.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'canceled', completed_at = $1, stop_reason = $2
		 WHERE repository_id = $3 AND run_id = $4`, now, reason, repositoryID, runID); err != nil {
		return nil, err
	}
	if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.canceled", nil, nil, nil, nil, nil, cancelPayload); err != nil {
		return nil, err
	}
	if err := closeRemainingSessions(ctx, tx, repositoryID, runID, "run_canceled", "run_canceled"); err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "state": "canceled", "status": "canceled"}, nil
}

func HandleRunCancel(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.cancel requires run_id", nil)
	}
	reason := stringParam(envelope, "reason")
	if reason == "" {
		reason = "operator_canceled"
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first — run.cancel locks runs then jobs
		// then sessions (closeRemainingSessions), inverting against the claim path.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		return cancelRunInTx(ctx, tx, repositoryID, runID, reason)
	})
}

func HandleRunRetryJob(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	jobID := stringParam(envelope, "job_id")
	if runID == "" || jobID == "" {
		return nil, rpc.NewError("schema_invalid", "run.retry_job requires run_id and job_id", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		// RFC 0104: per-run advisory lock first — run.retry_job re-opens a job
		// (reopenJobForAttempt) and re-blocks downstream, touching runs/jobs/leases/
		// sessions; serialize on the per-run lock.
		if err := lockRun(ctx, tx, repositoryID, runID); err != nil {
			return nil, err
		}
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		runState := fmt.Sprint(run["state"])
		if runState == "completed" || runState == "compromised" {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("run is %s; retry would revive a finished run. Start a replacement run after recording any invalidation decision.", runState), nil)
		}
		job, err := rowByID(ctx, tx, repositoryID, "jobs", "job_id", jobID, true)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(job["run_id"]) != runID {
			return nil, rpc.NewError("invalid_transition", "job does not belong to the requested run", nil)
		}
		previousState := fmt.Sprint(job["state"])
		retriable := previousState == "failed" || previousState == "canceled" || previousState == "blocked"
		// A `completed` job is retriable only when it is the declared target of
		// a revision cycle: re-opening a completed upstream synthesis/implement
		// job for a manual revision is a legitimate transition (F3, RFC 0083).
		// This keeps arbitrary completed jobs non-retriable while giving the
		// operator a path to drive a revision when auto-routing did not fire.
		isCycleTarget := false
		if !retriable && previousState == "completed" {
			cycleTarget, err := isDeclaredCycleTarget(ctx, tx, repositoryID, job)
			if err != nil {
				return nil, err
			}
			isCycleTarget = cycleTarget
			retriable = cycleTarget
		}
		if !retriable {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf("job state %q is not retriable (must be failed, canceled, or blocked, or a completed revision-cycle target)", previousState), nil)
		}
		// #273: reopenJobForAttempt bumps the attempt, so a recovery retry from
		// attempt N where N >= max_attempts silently exceeds the configured attempt
		// budget — during a worktree-durability recovery this minted a duplicate
		// attempt + lane instead of completing the remediated one. Refuse by default
		// once the budget is reached, point the operator at recovery.reseal (the
		// same-attempt path, RFC 0125), and require an explicit
		// --allow-exceed-max-attempts to override (recorded as an audited operator
		// override on job.retried). A declared revision-cycle-target reopen is
		// EXEMPT: the cycle's own max_iterations governs those rounds, not the
		// per-attempt max_attempts.
		attempt := intValue(job["attempt"])
		maxAttempts := intValue(job["max_attempts"])
		exceedsBudget := !isCycleTarget && maxAttempts > 0 && attempt >= maxAttempts
		allowExceed := boolParam(envelope, "allow_exceed_max_attempts")
		if exceedsBudget && !allowExceed {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"run.retry_job would bump job %s to attempt %d, exceeding max_attempts %d. For a remediated worktree-durability blocker use `recovery reseal` (completes the same attempt without a new one); to intentionally exceed the attempt budget pass --allow-exceed-max-attempts (recorded as an operator override).",
				jobID, attempt+1, maxAttempts), nil)
		}
		// RFC 0095 Phase 2 (#65 P3): re-open atomically through the single shared
		// helper so a retry can never leave a dangling active lease (the prior
		// implementation reset the job to queued + bumped the attempt but never
		// released the active lease, so a fresh `work.claim_next` would later fail
		// the uq_active_resource_lease index with `duplicate active job lease`).
		// reopenJobForAttempt releases the prior lease, cancels the prior message,
		// cancels open blockers, re-blocks transitive downstream terminal jobs +
		// clears their stale verdicts (a no-op for the typical failed/canceled/
		// blocked retry whose downstream is not terminal; the desired re-review
		// reset for a completed revision-cycle target), clears this job's stale
		// verdicts, bumps the attempt, and re-enqueues a fresh message.
		if _, err := reopenJobForAttempt(ctx, tx, repositoryID, job, "retry_job"); err != nil {
			return nil, err
		}
		runRevived := false
		if fmt.Sprint(run["state"]) == "failed" || fmt.Sprint(run["state"]) == "canceled" {
			if err := tx.Exec(ctx, `
				UPDATE striatumd.runs
				   SET state = 'running', completed_at = NULL, stop_reason = NULL
				 WHERE repository_id = $1 AND run_id = $2`, repositoryID, runID); err != nil {
				return nil, err
			}
			if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.revived", nil, nil, nil, nil, nil, map[string]any{
				"trigger_job_id":     jobID,
				"previous_run_state": run["state"],
			}); err != nil {
				return nil, err
			}
			runRevived = true
		}
		retriedPayload := map[string]any{
			"previous_state": previousState,
			"attempt":        intValue(job["attempt"]) + 1,
		}
		if exceedsBudget && allowExceed {
			// #273: a deliberate over-budget retry is recorded as an audited
			// operator override, not a silent attempt-budget bypass.
			retriedPayload["attempt_budget_override"] = true
			retriedPayload["max_attempts"] = maxAttempts
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "job.retried", nil, jobID, nil, nil, nil, retriedPayload); err != nil {
			return nil, err
		}
		return map[string]any{
			"run_id":         runID,
			"job_id":         jobID,
			"previous_state": previousState,
			"new_state":      "queued",
			"run_revived":    runRevived,
		}, nil
	})
}

func HandleBranchConfirm(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	branch := stringParam(envelope, "branch")
	if runID == "" || branch == "" {
		return nil, rpc.NewError("schema_invalid", "branch.confirm requires run_id and branch", nil)
	}
	create := boolParam(envelope, "create")
	useCurrent := boolParam(envelope, "use_current")
	strict := boolParam(envelope, "strict")
	if create && useCurrent {
		return nil, rpc.NewError("workflow_error", "--create and --use-current are mutually exclusive", nil)
	}
	if strict && (create || useCurrent) {
		return nil, rpc.NewError("workflow_error", "--strict is incompatible with --create and --use-current", nil)
	}
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		run, err := rowByID(ctx, tx, repositoryID, "runs", "run_id", runID, true)
		if err != nil {
			return nil, err
		}
		repoRoot := fmt.Sprint(run["repo_root"])
		requestedBranch := branch
		targetBranch := branch
		created := false
		mode := "records_only"
		branchBase := strings.TrimSpace(fmt.Sprint(run["branch_base"]))
		if branchBase == "<nil>" {
			branchBase = ""
		}
		if useCurrent {
			mode = "use_current"
			current := currentGitBranch(repoRoot)
			if current == "" {
				return nil, rpc.NewError("workflow_error", "--use-current requires a detectable current git branch in the target repo", nil)
			}
			if branch != current {
				return nil, rpc.NewError("workflow_error", fmt.Sprintf("--use-current was given but --branch=%q does not match current git branch %q", branch, current), nil)
			}
			targetBranch = current
		} else if create {
			mode = "create"
			var err error
			targetBranch, created, err = gitEnsureBranchRef(repoRoot, branch, branchBase)
			if err != nil {
				return nil, err
			}
		} else if strict {
			mode = "strict"
			current := currentGitBranch(repoRoot)
			if current != branch {
				return nil, rpc.NewError("workflow_error", fmt.Sprintf("--strict requires current git branch to match --branch=%q; current branch is %q", branch, current), nil)
			}
		} else {
			var err error
			targetBranch, created, err = gitEnsureBranchRef(repoRoot, branch, branchBase)
			if err != nil {
				return nil, err
			}
		}
		state := fmt.Sprint(run["state"])
		if state != "needs_branch_confirmation" && state != "ready" {
			return nil, rpc.NewError("invalid_transition", "run is not waiting for branch confirmation", nil)
		}
		currentBranch := currentGitBranch(repoRoot)
		now := nowString()
		if err := tx.Exec(ctx, `
			UPDATE striatumd.runs
			   SET branch_name = $1, branch_confirmed_at = $2,
			       branch_confirmed_by = 'human', state = 'ready'
			 WHERE repository_id = $3 AND run_id = $4`, targetBranch, now, repositoryID, runID); err != nil {
			return nil, err
		}
		if _, err := appendEvent(ctx, tx, repositoryID, runID, "run.branch_confirmed", nil, nil, nil, nil, nil, map[string]any{
			"branch":  targetBranch,
			"mode":    mode,
			"created": created,
		}); err != nil {
			return nil, err
		}
		var warning any
		if currentBranch != "" && currentBranch != targetBranch {
			warning = "current git branch differs from recorded branch confirmation"
		}
		return map[string]any{
			"run_id":             runID,
			"state":              "ready",
			"branch":             targetBranch,
			"requested_branch":   requestedBranch,
			"current_git_branch": nullable(currentBranch),
			"records_only":       !created,
			"warning":            warning,
			"created":            created,
			"mode":               mode,
		}, nil
	})
}

func currentGitBranch(repoRoot string) string {
	result := exec.Command("git", "branch", "--show-current")
	result.Dir = repoRoot
	out, err := result.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func currentGitHead(repoRoot string) string {
	result := exec.Command("git", "rev-parse", "--verify", "HEAD")
	result.Dir = repoRoot
	out, err := result.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitBranchExists reports whether a local branch with the given name exists in
// the repository at repoRoot. It uses "git rev-parse --verify" which exits 0
// iff the ref resolves.
func gitBranchExists(repoRoot string, branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = repoRoot
	return cmd.Run() == nil
}

func gitEnsureBranchRef(repoRoot string, branch string, base string) (string, bool, error) {
	if gitBranchExists(repoRoot, branch) {
		return branch, false, nil
	}
	base = strings.TrimSpace(base)
	if base == "" || base == "<nil>" {
		base = "HEAD"
	}
	create := exec.Command("git", "branch", branch, base)
	create.Dir = repoRoot
	out, err := create.CombinedOutput()
	if err == nil {
		return branch, true, nil
	}
	stderr := strings.TrimSpace(string(out))
	if len(stderr) > 200 {
		stderr = stderr[:200] + "..."
	}
	if stderr != "" {
		return "", false, rpc.NewError("workflow_error", fmt.Sprintf("git branch failed for branch %q at base %q: %s", branch, base, stderr), nil)
	}
	return "", false, rpc.NewError("workflow_error", fmt.Sprintf("git branch failed for branch %q at base %q", branch, base), nil)
}

func runPrepare(ctx context.Context, runner any, repositoryID string, workflowPath string) (map[string]any, error) {
	repo, err := rowByID(ctx, runner, repositoryID, "repositories", "repository_id", repositoryID, false)
	if err != nil {
		return nil, err
	}
	repoRoot := fmt.Sprint(repo["repo_root"])
	workflow, sourcePath, err := workflowauthoring.LoadFile(repoRoot, workflowPath)
	if err != nil {
		return nil, rpc.NewError("workflow_error", err.Error(), nil)
	}
	workflowID, _ := workflow["workflow_id"].(string)
	if workflowID == "" {
		return nil, rpc.NewError("workflow_error", "workflow config must declare workflow_id", nil)
	}
	phaseIndex, err := validateWorkflowForPrepare(workflow)
	if err != nil {
		return nil, err
	}
	now := nowString()
	normalized, err := json.Marshal(workflow)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(normalized)
	snapshotID, err := newID("wfs")
	if err != nil {
		return nil, err
	}
	runID, err := newID("run")
	if err != nil {
		return nil, err
	}
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return nil, fmt.Errorf("runner does not support exec")
	}
	workflowJSONArg, err := db.JSONBArg(runner, workflow)
	if err != nil {
		return nil, err
	}
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.workflow_snapshots (
		  repository_id, workflow_snapshot_id, workflow_id, workflow_version,
		  source_path, content_sha256, workflow_json, loaded_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`,
		repositoryID,
		snapshotID,
		workflowID,
		nullable(workflow["workflow_version"]),
		sourcePath,
		hex.EncodeToString(sum[:]),
		workflowJSONArg,
		now,
	); err != nil {
		return nil, err
	}
	branch := asMap(workflow["branch"])
	suggestedBranch, _ := branch["suggested_name"].(string)
	state := "needs_branch_confirmation"
	var confirmedAt any
	var confirmedBy any
	branchBase := currentGitHead(repoRoot)
	// GH #123/#183: auto branch confirmation must verify the branch ref actually
	// exists before recording it as confirmed. Without this check,
	// run.prepare can record a confirmed branch that was never created, causing
	// subsequent git operations (commit-apply, worktree.create) to silently
	// operate on the wrong branch. Create the ref without moving the operator's
	// primary HEAD; if the git operation fails, surface the error rather than
	// recording a ghost confirmation.
	autoConfirmCreated := false
	if branch["mode"] == "auto" && suggestedBranch != "" {
		_, created, createErr := gitEnsureBranchRef(repoRoot, suggestedBranch, branchBase)
		if createErr != nil {
			return nil, rpc.NewError("workflow_error", fmt.Sprintf("auto branch confirm: could not ensure branch %q: %s", suggestedBranch, createErr.Error()), nil)
		}
		autoConfirmCreated = created
		state = "ready"
		confirmedAt = now
		confirmedBy = "daemon"
	}
	_ = autoConfirmCreated // recorded in the branch_confirmed event below
	if err := exec.Exec(ctx, `
		INSERT INTO striatumd.runs (
		  repository_id, run_id, workflow_snapshot_id, repo_root, state,
		  branch_name, branch_base, branch_confirmed_at, branch_confirmed_by,
		  created_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		repositoryID,
		runID,
		snapshotID,
		repoRoot,
		state,
		nullable(suggestedBranch),
		nullable(branchBase),
		confirmedAt,
		confirmedBy,
		now,
	); err != nil {
		return nil, err
	}
	jobIDs := map[string]string{}
	for _, item := range asList(workflow["jobs"]) {
		job := asMap(item)
		workflowJobID, _ := job["id"].(string)
		if workflowJobID == "" {
			return nil, rpc.NewError("workflow_error", "workflow job is missing id", nil)
		}
		jobID := fmt.Sprintf("job_%s_%s", runID, workflowJobID)
		jobIDs[workflowJobID] = jobID
		rawType, _ := job["type"].(string)
		jobType := storedJobType(rawType)
		capabilityReqs := map[string]any{
			"objective":   job["objective"],
			"task_prompt": asMap(job["task_prompt"]),
			"inputs":      asList(job["inputs"]),
		}
		laneID, _ := job["lane_id"].(string)
		laneSelector := map[string]any{}
		if laneID != "" {
			laneSelector["lane_id"] = laneID
		}
		laneSelectorArg, err := db.JSONBArg(runner, laneSelector)
		if err != nil {
			return nil, err
		}
		capabilityReqsArg, err := db.JSONBArg(runner, capabilityReqs)
		if err != nil {
			return nil, err
		}
		writeScopeArg, err := db.JSONBArg(runner, valueOr(job["write_scope"], map[string]any{}))
		if err != nil {
			return nil, err
		}
		expectedArtifactsArg, err := db.JSONBArg(runner, valueOr(job["expected_artifacts"], []any{}))
		if err != nil {
			return nil, err
		}
		if err := exec.Exec(ctx, `
			INSERT INTO striatumd.jobs (
			  repository_id, job_id, run_id, workflow_job_id, title, job_type,
			  role_id, lane_selector_json, capability_requirements_json, state,
			  max_attempts, fresh_session_required, write_scope_json,
			  expected_artifacts_json, idempotency_key, created_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,'blocked',$10,$11,$12::jsonb,$13::jsonb,$14,$15)`,
			repositoryID,
			jobID,
			runID,
			workflowJobID,
			valueOr(job["title"], workflowJobID),
			jobType,
			job["role_id"],
			laneSelectorArg,
			capabilityReqsArg,
			valueOr(job["max_attempts"], 1),
			effectiveFreshSessionRequired(job),
			writeScopeArg,
			expectedArtifactsArg,
			fmt.Sprintf("%s:%s:1", runID, workflowJobID),
			now,
		); err != nil {
			return nil, err
		}
	}
	for _, edge := range edgeDependencyPairs(workflow, phaseIndex, true) {
		fromID := edge.fromID
		toID := edge.toID
		fromJobID := jobIDs[fromID]
		toJobID := jobIDs[toID]
		if fromJobID == "" || toJobID == "" {
			return nil, rpc.NewError("workflow_error", "workflow edge references an unknown job", nil)
		}
		gate := map[string]any{"on": "completed", "from": fromID, "to": toID}
		if edgeRequiresClearingVerdict(workflow, fromID, toID) {
			gate["requires_verdict"] = []string{"accept", "accept_with_findings"}
		}
		gateArg, err := db.JSONBArg(runner, gate)
		if err != nil {
			return nil, err
		}
		if err := exec.Exec(ctx, `
			INSERT INTO striatumd.job_dependencies (
			  repository_id, job_id, depends_on_job_id, gate_json
			)
			VALUES ($1,$2,$3,$4::jsonb)
			ON CONFLICT (repository_id, job_id, depends_on_job_id) DO NOTHING`,
			repositoryID, toJobID, fromJobID, gateArg); err != nil {
			return nil, err
		}
	}
	if _, err := appendEvent(ctx, runner, repositoryID, runID, "run.created", nil, nil, nil, nil, nil, map[string]any{
		"workflow_id":          workflowID,
		"workflow_snapshot_id": snapshotID,
	}); err != nil {
		return nil, err
	}
	if state == "ready" {
		if _, err := appendEvent(ctx, runner, repositoryID, runID, "run.branch_confirmed", nil, nil, nil, nil, nil, map[string]any{
			"branch":  suggestedBranch,
			"mode":    "auto",
			"created": autoConfirmCreated,
		}); err != nil {
			return nil, err
		}
	}
	branchMode, _ := branch["mode"].(string)
	if branchMode == "" {
		branchMode = "auto"
	}
	return map[string]any{
		"run_id":                runID,
		"state":                 state,
		"branch_mode":           branchMode,
		"suggested_branch_name": nullable(suggestedBranch),
	}, nil
}

func workflowForRun(ctx context.Context, runner any, repositoryID string, run map[string]any) (map[string]any, error) {
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return nil, err
	}
	return asMap(snapshot["workflow_json"]), nil
}

// workflowSnapshotDivergence reports a #115 warning when a run's FROZEN workflow
// snapshot (captured at run.prepare) differs from the current on-disk workflow
// file at the snapshot's source_path. A prepared/running run uses the snapshot, so
// edits to the file are inert; surfacing the divergence stops an operator burning
// time on a silent no-op (the fix is to prepare a new run). The comparison is over
// the canonical json.Marshal form — exactly how run.prepare computed the snapshot
// sha — so cosmetic file changes (whitespace, key order) do not false-trigger.
// Returns "" when there is no divergence OR it cannot be determined (no
// source_path/sha, file gone/unreadable): only a positive mismatch warns.
func workflowSnapshotDivergence(repoRoot, sourcePath, snapshotSha string) string {
	if snapshotSha == "" || snapshotSha == "<nil>" || sourcePath == "" || sourcePath == "<nil>" {
		return ""
	}
	current, _, err := workflowauthoring.LoadFile(repoRoot, sourcePath)
	if err != nil {
		return ""
	}
	normalized, err := json.Marshal(current)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(normalized)
	if hex.EncodeToString(sum[:]) == snapshotSha {
		return ""
	}
	return fmt.Sprintf("on-disk workflow %s diverges from this run's frozen snapshot; the run uses the snapshot, so edits to the file will NOT apply — prepare a NEW run to change lanes/commands (#115)", sourcePath)
}

// snapshotDivergenceWarningForRun loads a run's snapshot row and returns the #115
// divergence warning (or "" on any error — best-effort; never fails the caller).
func snapshotDivergenceWarningForRun(ctx context.Context, runner db.Runner, repositoryID, repoRoot, workflowSnapshotID string) string {
	if workflowSnapshotID == "" {
		return ""
	}
	snap, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", workflowSnapshotID, false)
	if err != nil {
		return ""
	}
	return workflowSnapshotDivergence(repoRoot, fmt.Sprint(nullable(snap["source_path"])), fmt.Sprint(nullable(snap["content_sha256"])))
}

func effectiveFreshSessionRequired(job map[string]any) bool {
	if job["fresh_session_required"] == true {
		return true
	}
	_, hasExplicit := job["fresh_session_required"]
	return job["type"] == "review" && job["reviewer_context_policy"] == "fresh" && !hasExplicit
}

// storedJobType maps a workflow-authoring job `type` to the DB job_type the
// jobs_job_type_check constraint accepts. The workflow snapshot keeps the
// authoring-level type (the value validatePhaseGateSequencing / edge gating key
// on); this is the prepare-time projection onto the storage enum:
//   - phase_synthesis is a verdict-emitting synthesis stored as `review` (the
//     job-row type a review.verdict transition expects).
//   - proposal (RFC 0094 §2 work-packet type sequencing) is a build-class
//     deliverable whose EMISSION is withheld behind a coverage gate; stored as
//     `build`.
//   - an empty type defaults to `generic`.
//
// Every other type is already in the constraint set and passes through unchanged.
func storedJobType(rawType string) string {
	switch rawType {
	case "":
		return "generic"
	case "phase_synthesis":
		return "review"
	case "proposal":
		return "build"
	default:
		return rawType
	}
}

func workflowJobType(workflow map[string]any, workflowJobID string) string {
	for _, item := range asList(workflow["jobs"]) {
		job := asMap(item)
		if job["id"] == workflowJobID {
			typ, _ := job["type"].(string)
			return typ
		}
	}
	return ""
}

// edgeRequiresClearingVerdict decides whether a from→to dependency edge gates
// the downstream on a clearing verdict (#77). An edge from a verdict-capable job
// (review / phase_synthesis) normally requires accept/accept_with_findings — but
// an edge INTO an adjudicator (a phase_synthesis job) stays ungated, because the
// adjudicator's role is to weigh every input verdict (including a reviewer's
// needs_revision dissent) rather than be pre-gated by it.
func edgeRequiresClearingVerdict(workflow map[string]any, fromID, toID string) bool {
	fromType := workflowJobType(workflow, fromID)
	if fromType != "review" && fromType != "phase_synthesis" {
		return false
	}
	return workflowJobType(workflow, toID) != "phase_synthesis"
}

type phaseIndex struct {
	declared         bool
	phaseOrder       []string
	phasePosition    map[string]int
	jobPhase         map[string]string
	synthesisByPhase map[string]string
}

type dependencyEdge struct {
	fromID string
	toID   string
}

func validateWorkflowForPrepare(workflow map[string]any) (phaseIndex, error) {
	for _, field := range []string{"schema_version", "workflow_id", "branch", "lanes", "roles", "jobs"} {
		if _, ok := workflow[field]; !ok {
			return phaseIndex{}, rpc.NewError("workflow_error", "workflow is missing required field "+field, nil)
		}
	}
	schemaVersion, _ := workflow["schema_version"].(string)
	if schemaVersion != "striatum.workflow.v1" && schemaVersion != "striatum.workflow.v1.1" {
		return phaseIndex{}, rpc.NewError("workflow_error", "workflow schema_version must be one of: striatum.workflow.v1, striatum.workflow.v1.1", nil)
	}
	roles := asMap(workflow["roles"])
	lanes := asMap(workflow["lanes"])
	jobs := workflowJobDefinitions(workflow)
	seen := map[string]bool{}
	for index, job := range jobs {
		jobID, _ := job["id"].(string)
		if jobID == "" {
			return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("jobs[%d].id must be a non-empty string", index), nil)
		}
		if seen[jobID] {
			return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("duplicate job id %q", jobID), nil)
		}
		seen[jobID] = true
		roleID, _ := job["role_id"].(string)
		if roleID == "" || roles[roleID] == nil {
			return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("job %q references unknown role %q", jobID, roleID), nil)
		}
		if laneID, ok := job["lane_id"].(string); ok && laneID != "" && lanes[laneID] == nil {
			return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("job %q references unknown lane %q", jobID, laneID), nil)
		}
		for _, artifactValue := range asList(job["expected_artifacts"]) {
			artifact := asMap(artifactValue)
			path, _ := artifact["path"].(string)
			if path == "" || filepath.IsAbs(path) || strings.Contains(path, "..") {
				return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("job %q has invalid artifact path", jobID), nil)
			}
			if kind, ok := artifact["kind"].(string); ok && kind != "" && !allowedArtifactKinds[kind] {
				return phaseIndex{}, rpc.NewError("workflow_error", fmt.Sprintf("job %s declares unknown artifact kind %s", jobID, kind), nil)
			}
		}
	}
	// Phase-shape rules are the single source of truth in
	// pkg/workflowauthoring (GH #66) so `workflow validate` rejects the same
	// shapes locally. workflowPhaseIndex/validatePhaseEdges below build the
	// materialization index and re-run the identical checks as defense.
	if err := workflowauthoring.ValidatePhaseShapes(workflow); err != nil {
		return phaseIndex{}, rpc.NewError("workflow_error", err.Error(), nil)
	}
	// Refuse retired one-shot agent commands at prepare so a poisoned workflow
	// can never reach a launched lane. Claude's compatibility override remains
	// lane-local; Codex exec has no override because it cannot ack reliably.
	if err := workflowauthoring.RefuseRetiredOneShotLanes(workflow); err != nil {
		return phaseIndex{}, rpc.NewError("workflow_error", err.Error(), nil)
	}
	if err := workflowauthoring.RefuseAutonomousSharedCheckoutRepoWrite(workflow); err != nil {
		return phaseIndex{}, rpc.NewError("workflow_error", err.Error(), nil)
	}
	index, err := workflowPhaseIndex(workflow, jobs, schemaVersion)
	if err != nil {
		return phaseIndex{}, err
	}
	for _, edge := range edgeDependencyPairs(workflow, index, false) {
		if !seen[edge.fromID] || !seen[edge.toID] {
			return phaseIndex{}, rpc.NewError("workflow_error", "workflow edge references an unknown job", nil)
		}
	}
	if err := validateWorkflowAugmentation(workflow, seen); err != nil {
		return phaseIndex{}, err
	}
	if err := validatePhaseEdges(workflow, index); err != nil {
		return phaseIndex{}, err
	}
	return index, nil
}

func validateWorkflowAugmentation(workflow map[string]any, jobs map[string]bool) error {
	raw, ok := workflow["augmentation"]
	if !ok || raw == nil {
		return nil
	}
	augmentation := asMap(raw)
	if len(augmentation) == 0 {
		return rpc.NewError("workflow_error", "workflow augmentation must be an object", nil)
	}
	mode, _ := augmentation["mode"].(string)
	if mode != "reference_only" {
		return rpc.NewError("workflow_error", "workflow augmentation.mode must be reference_only", nil)
	}
	if required, ok := augmentation["required"]; ok {
		requiredValue, isBool := required.(bool)
		if !isBool || requiredValue {
			return rpc.NewError("workflow_error", "workflow augmentation.required must be false", nil)
		}
	}
	if budget, ok := augmentation["budget_per_packet_lines"]; ok {
		budgetValue, validBudget := augmentationBudgetValue(budget)
		if !validBudget || budgetValue <= 0 || budgetValue > 5000 {
			return rpc.NewError("workflow_error", "workflow augmentation.budget_per_packet_lines must be a positive integer <= 5000", nil)
		}
	}
	sources := asList(augmentation["sources"])
	if len(sources) == 0 {
		return rpc.NewError("workflow_error", "workflow augmentation.sources must be a non-empty list", nil)
	}
	sourceIDs := map[string]bool{}
	for index, item := range sources {
		source := asMap(item)
		sourceID, _ := source["id"].(string)
		if sourceID == "" || !augmentationSourceIDPattern.MatchString(sourceID) {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow augmentation.sources[%d].id must be a non-empty local identifier", index), nil)
		}
		if sourceIDs[sourceID] {
			return rpc.NewError("workflow_error", fmt.Sprintf("duplicate workflow augmentation source id %q", sourceID), nil)
		}
		sourceIDs[sourceID] = true
		if source["kind"] != "corpus_bundle" {
			return rpc.NewError("workflow_error", "workflow augmentation source kind must be corpus_bundle", nil)
		}
		pathText, _ := source["path"].(string)
		if safeAugmentationPath(pathText) == "" {
			return rpc.NewError("workflow_error", "workflow augmentation source path must be repo-relative without '..' and outside .striatum/", nil)
		}
		if description, ok := source["description"]; ok {
			if _, isString := description.(string); !isString {
				return rpc.NewError("workflow_error", "workflow augmentation source description must be a string", nil)
			}
		}
	}
	augmentedJobs := asList(augmentation["jobs"])
	if len(augmentedJobs) == 0 {
		return rpc.NewError("workflow_error", "workflow augmentation.jobs must be a non-empty list", nil)
	}
	seenJobs := map[string]bool{}
	for index, item := range augmentedJobs {
		jobID, ok := item.(string)
		if !ok || jobID == "" {
			return rpc.NewError("workflow_error", "workflow augmentation.jobs entries must be non-empty strings", nil)
		}
		if seenJobs[jobID] {
			return rpc.NewError("workflow_error", fmt.Sprintf("duplicate workflow augmentation job %q", jobID), nil)
		}
		seenJobs[jobID] = true
		if !jobs[jobID] {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow augmentation.jobs[%d] references unknown job %q", index, jobID), nil)
		}
	}
	return nil
}

func augmentationBudgetValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		intValue := int(typed)
		return intValue, typed == float64(intValue)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	}
	return 0, false
}

func workflowJobDefinitions(workflow map[string]any) []map[string]any {
	result := []map[string]any{}
	for _, item := range asList(workflow["jobs"]) {
		result = append(result, asMap(item))
	}
	return result
}

func workflowPhaseIndex(workflow map[string]any, jobs []map[string]any, schemaVersion string) (phaseIndex, error) {
	empty := phaseIndex{phasePosition: map[string]int{}, jobPhase: map[string]string{}, synthesisByPhase: map[string]string{}}
	phases := asList(workflow["phases"])
	if schemaVersion == "striatum.workflow.v1" {
		if len(phases) > 0 {
			return empty, rpc.NewError("workflow_error", "striatum.workflow.v1 workflows must not declare phases", nil)
		}
		for _, job := range jobs {
			if _, ok := job["phase_id"]; ok {
				return empty, rpc.NewError("workflow_error", fmt.Sprintf("striatum.workflow.v1 job %q must not declare phase_id", job["id"]), nil)
			}
			if job["type"] == "phase_synthesis" {
				return empty, rpc.NewError("workflow_error", fmt.Sprintf("striatum.workflow.v1 job %q must not use type phase_synthesis", job["id"]), nil)
			}
		}
		return empty, nil
	}
	if len(phases) == 0 {
		for _, job := range jobs {
			if _, ok := job["phase_id"]; ok {
				return empty, rpc.NewError("workflow_error", fmt.Sprintf("job %q may declare phase_id only when workflow phases are declared", job["id"]), nil)
			}
			if job["type"] == "phase_synthesis" {
				return empty, rpc.NewError("workflow_error", fmt.Sprintf("job %q may use type phase_synthesis only when workflow phases are declared", job["id"]), nil)
			}
		}
		return empty, nil
	}
	index := phaseIndex{declared: true, phasePosition: map[string]int{}, jobPhase: map[string]string{}, synthesisByPhase: map[string]string{}}
	phaseSeen := map[string]bool{}
	for pos, phaseValue := range phases {
		phase := asMap(phaseValue)
		phaseID, _ := phase["id"].(string)
		if phaseID == "" || phaseSeen[phaseID] {
			return empty, rpc.NewError("workflow_error", "phase id must be unique and non-empty", nil)
		}
		phaseSeen[phaseID] = true
		index.phaseOrder = append(index.phaseOrder, phaseID)
		index.phasePosition[phaseID] = pos
	}
	jobCountByPhase := map[string]int{}
	for _, job := range jobs {
		jobID, _ := job["id"].(string)
		phaseID, _ := job["phase_id"].(string)
		if phaseID == "" || !phaseSeen[phaseID] {
			return empty, rpc.NewError("workflow_error", fmt.Sprintf("job %q references unknown phase_id %q", jobID, phaseID), nil)
		}
		index.jobPhase[jobID] = phaseID
		jobCountByPhase[phaseID]++
		if job["type"] != "phase_synthesis" {
			continue
		}
		if existing := index.synthesisByPhase[phaseID]; existing != "" {
			return empty, rpc.NewError("workflow_error", fmt.Sprintf("phase %q has multiple phase_synthesis jobs: %s, %s", phaseID, existing, jobID), nil)
		}
		index.synthesisByPhase[phaseID] = jobID
	}
	for _, phaseID := range index.phaseOrder {
		if index.synthesisByPhase[phaseID] == "" {
			return empty, rpc.NewError("workflow_error", fmt.Sprintf("phase %q must declare exactly one phase_synthesis job", phaseID), nil)
		}
		if jobCountByPhase[phaseID] < 2 {
			return empty, rpc.NewError("workflow_error", fmt.Sprintf("phase %q phase_synthesis job must have at least one peer job", phaseID), nil)
		}
	}
	return index, nil
}

func edgeDependencyPairs(workflow map[string]any, index phaseIndex, includePhaseMaterialized bool) []dependencyEdge {
	pairs := []dependencyEdge{}
	seen := map[string]bool{}
	for _, edgeValue := range asList(workflow["edges"]) {
		edge := asMap(edgeValue)
		fromID, _ := edge["from"].(string)
		toID, _ := edge["to"].(string)
		if fromID == "" || toID == "" || edge["on"] != "completed" {
			continue
		}
		key := fromID + "\x00" + toID
		if seen[key] {
			continue
		}
		seen[key] = true
		pairs = append(pairs, dependencyEdge{fromID: fromID, toID: toID})
	}
	if includePhaseMaterialized && index.declared {
		for _, job := range workflowJobDefinitions(workflow) {
			jobID, _ := job["id"].(string)
			phaseID := index.jobPhase[jobID]
			synthesisID := index.synthesisByPhase[phaseID]
			if jobID == "" || jobID == synthesisID {
				continue
			}
			key := jobID + "\x00" + synthesisID
			if seen[key] {
				continue
			}
			seen[key] = true
			pairs = append(pairs, dependencyEdge{fromID: jobID, toID: synthesisID})
		}
	}
	return pairs
}

func validatePhaseEdges(workflow map[string]any, index phaseIndex) error {
	if !index.declared {
		return nil
	}
	jobTypes := map[string]string{}
	for _, job := range workflowJobDefinitions(workflow) {
		jobID, _ := job["id"].(string)
		jobType, _ := job["type"].(string)
		jobTypes[jobID] = jobType
	}
	for _, edge := range edgeDependencyPairs(workflow, index, false) {
		fromPhase := index.jobPhase[edge.fromID]
		toPhase := index.jobPhase[edge.toID]
		if fromPhase == "" || toPhase == "" || fromPhase == toPhase {
			continue
		}
		fromPos := index.phasePosition[fromPhase]
		toPos := index.phasePosition[toPhase]
		if toPos < fromPos {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow edge %q -> %q points from later phase %q to earlier phase %q", edge.fromID, edge.toID, fromPhase, toPhase), nil)
		}
		if toPos != fromPos+1 {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow edge %q -> %q skips phases; cross-phase edges may target only the immediate next phase", edge.fromID, edge.toID), nil)
		}
		if index.synthesisByPhase[fromPhase] != edge.fromID {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow edge %q -> %q crosses phases without using source phase %q synthesis job", edge.fromID, edge.toID, fromPhase), nil)
		}
		if jobTypes[edge.toID] == "phase_synthesis" {
			return rpc.NewError("workflow_error", fmt.Sprintf("workflow edge %q -> %q cannot target a later phase_synthesis job", edge.fromID, edge.toID), nil)
		}
	}
	return nil
}

func valueOr(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	if text, ok := value.(string); ok && text == "" {
		return fallback
	}
	return value
}
