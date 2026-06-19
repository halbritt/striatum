package reads

import (
	"context"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
)

// doctorStuckJobNoLiveSessionAfter is the conservative quiet bound past which a
// non-terminal job WITHOUT a live session and WITHOUT recent progress is flagged
// as a likely wedge (#373/#388/#389). It is intentionally longer than the
// recovery-sweep wedge bound (5m): a healthy `running` lane can legitimately be
// quiet for several minutes doing local work, so this check trades latency for a
// near-zero false-positive rate. It is a WARNING, not a hard red, for the same
// reason — it must never re-red a green baseline on a normal in-flight job.
const doctorStuckJobNoLiveSessionAfter = 15 * time.Minute

// doctorStuckJobStatesSQL is the SQL list literal of non-terminal job states
// that should be making progress on a live lane. A job sitting in one of these
// states with no live session and no recent progress is the cross-cutting wedge
// family that was previously invisible to doctor:
//   - running / claimed / stale_lease with a dead session (#388 sessionless
//     running job): the recovery_sweep_cursor check only fires when
//     claimable_job_count > 0, so a sessionless in-flight job stays green.
//   - blocked (#389): a transient publish/seal failure leaves the job blocked
//     with no claimable count at all, so doctor stayed green while the run
//     deadlocked.
//
// It is a hard-coded SQL literal (not a bound parameter) of a fixed, trusted
// constant set — no caller input flows into it.
const doctorStuckJobStatesSQL = `('running','claimed','stale_lease','blocked')`

// doctorStuckJobsNoLiveSession flags jobs stuck in a non-terminal state with NO
// live (active) session AND no recent progress past the conservative bound. It
// surfaces the #373/#388/#389 wedge family that doctor previously could not see
// because the only wedge check keyed on claimable_job_count > 0.
//
// It is deliberately conservative to avoid false positives on healthy
// actively-running jobs:
//   - it considers ONLY jobs whose run is still `running`;
//   - a job with ANY active session on its run+role+lane is treated as live and
//     skipped (a job whose lease owner session is active, or whose lane simply
//     has a live session, is healthy);
//   - a job whose freshest progress signal (job started_at/ready_at, the current
//     lease's last_heartbeat_at, or any heartbeat on a session for the job's
//     run+role+lane) is within the bound is skipped;
//   - the result is a WARNING (never a hard red), so normal in-flight latency
//     does not flip `ok` to false.
func doctorStuckJobsNoLiveSession(ctx context.Context, runner db.Runner, repositoryID string, now time.Time) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked":           false,
		"check":             "job_stuck_no_live_session",
		"skipped":           "repository_id required",
		"threshold_seconds": int(doctorStuckJobNoLiveSessionAfter.Seconds()),
		"stuck_count":       0,
		"stuck_jobs":        []map[string]any{},
	}
	if repositoryID == "" {
		return block, nil, nil
	}
	quietBefore := now.UTC().Add(-doctorStuckJobNoLiveSessionAfter)
	rows, err := collectRows(ctx, runner, `
		WITH stuck AS (
			SELECT j.job_id,
			       j.run_id,
			       j.workflow_job_id,
			       j.role_id,
			       j.state AS job_state,
			       COALESCE(j.lane_selector_json->>'lane_id', '') AS lane_id,
			       j.current_lease_id,
			       j.started_at,
			       j.ready_at
			  FROM striatumd.jobs j
			  JOIN striatumd.runs r
			    ON r.repository_id = j.repository_id
			   AND r.run_id = j.run_id
			 WHERE j.repository_id = $1
			   AND r.state = 'running'
			   AND j.state IN `+doctorStuckJobStatesSQL+`
		)
		SELECT s.job_id,
		       s.run_id,
		       s.workflow_job_id,
		       s.role_id,
		       s.lane_id,
		       s.job_state,
		       lease.last_heartbeat_at AS lease_last_heartbeat_at,
		       s.started_at,
		       s.ready_at,
		       live.live_session_count,
		       live.last_session_progress_at
		  FROM stuck s
		  LEFT JOIN striatumd.leases lease
		    ON lease.repository_id = $1
		   AND lease.lease_id = s.current_lease_id
		  LEFT JOIN LATERAL (
		    SELECT COUNT(*) FILTER (WHERE sess.state = 'active') AS live_session_count,
		           MAX(GREATEST(
		             sess.last_heartbeat_at,
		             sess.last_session_heartbeat_at,
		             sess.last_work_heartbeat_at,
		             sess.last_mcp_request_at,
		             sess.last_pty_activity_at
		           )) AS last_session_progress_at
		      FROM striatumd.sessions sess
		     WHERE sess.repository_id = $1
		       AND sess.run_id = s.run_id
		       AND sess.role_id = s.role_id
		       AND sess.lane_id = s.lane_id
		  ) live ON true
		 ORDER BY s.run_id, s.workflow_job_id, s.job_id`,
		repositoryID)
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		// Read failure is a warning, not a hard red: this is an advisory legibility
		// check and must not red an otherwise-green daemon on a probe hiccup.
		return block, []string{"job_stuck_no_live_session.read_failed: " + err.Error()}, []map[string]any{{
			"check": "job_stuck_no_live_session_read_failed",
			"error": err.Error(),
		}}
	}
	block["checked"] = true
	block["skipped"] = nil

	stuck := []map[string]any{}
	warnings := []string{}
	records := []map[string]any{}
	for _, row := range rows {
		// A live (active) session on the job's run+role+lane means the lane is
		// alive — never flag it (guards against false positives on healthy jobs).
		if intFrom(row, "live_session_count") > 0 {
			continue
		}
		quietSince, ok := stuckJobQuietSince(row)
		if !ok {
			// No progress signal at all and no live session: treat the job's run
			// start as the quiet anchor is impossible here, so fall back to flagging
			// only when we have a concrete quiet timestamp. Without ANY timestamp we
			// cannot bound staleness, so we conservatively skip rather than guess.
			continue
		}
		if !quietSince.Before(quietBefore) {
			continue
		}
		jobID := stringFrom(row, "job_id")
		runID := stringFrom(row, "run_id")
		jobState := stringFrom(row, "job_state")
		remediation := stuckJobRemediation(jobState, row["started_at"])
		record := map[string]any{
			"check":             "job_stuck_no_live_session",
			"job_id":            jobID,
			"run_id":            runID,
			"workflow_job_id":   stringFrom(row, "workflow_job_id"),
			"role_id":           stringFrom(row, "role_id"),
			"lane_id":           stringFrom(row, "lane_id"),
			"job_state":         jobState,
			"quiet_since":       quietSince.UTC().Format(time.RFC3339),
			"threshold_seconds": int(doctorStuckJobNoLiveSessionAfter.Seconds()),
			"remediation":       remediation,
		}
		stuck = append(stuck, record)
		records = append(records, record)
		warnings = append(warnings, "job_stuck_no_live_session."+jobID+": job_state="+jobState+
			" with no live session and no progress since "+record["quiet_since"].(string)+
			"; "+remediation)
	}
	block["stuck_count"] = len(stuck)
	block["stuck_jobs"] = stuck
	return block, warnings, records
}

// stuckJobQuietSince returns the freshest progress timestamp across the job's own
// timestamps, its current lease's heartbeat, and any session for its
// run+role+lane. The job is quiet since that moment. Returns false when no
// timestamp is available to bound staleness.
func stuckJobQuietSince(row map[string]any) (time.Time, bool) {
	candidates := []any{
		row["last_session_progress_at"],
		row["lease_last_heartbeat_at"],
		row["started_at"],
		row["ready_at"],
	}
	var newest time.Time
	found := false
	for _, candidate := range candidates {
		if ts, ok := parseTimeValue(candidate); ok {
			if !found || ts.After(newest) {
				newest = ts
				found = true
			}
		}
	}
	return newest, found
}

// stuckJobRemediation names the verb(s) that unstick a job in the given state.
// startedAt is the job's started_at column value (nil or "" means the job never
// started, i.e. it is dependency-blocked rather than seal-failed).
func stuckJobRemediation(jobState string, startedAt any) string {
	switch jobState {
	case "blocked":
		if startedAt != nil && startedAt != "" {
			// The job started (a lane claimed and ran it) but a transient
			// publish/seal failure left it blocked. `recovery reseal` re-queues
			// the same attempt; `recovery resolve-blocker` clears a dangling
			// blocker that no completion path cleared.
			return "run `striatum recovery reseal --run-id <run> --job-id <job>` (lane finished but the seal failed) or `striatum recovery resolve-blocker <blocker-id>`"
		}
		// The job has never started: it is blocked waiting on an upstream
		// dependency, not because a seal failed. `recovery resolve-blocker`
		// clears a dangling blocker; inspect with `striatum why` to identify
		// the unsatisfied dependency.
		return "job is blocked waiting on an upstream dependency; inspect with `striatum why` to identify the unsatisfied dependency or run `striatum recovery resolve-blocker <blocker-id>` if a blocker is dangling"
	case "stale_lease":
		return "run `striatum recovery requeue-stale` to reclaim the lease, or `striatum recovery complete-stalled --run-id <run> --job-id <job>` if the work is already durable"
	default: // running / claimed
		return "the lane has no live session; run `striatum recovery complete-stalled --run-id <run> --job-id <job>` if the work is durable, or `striatum recovery requeue-stale` to re-drive it"
	}
}
