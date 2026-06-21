package reads

import (
	"context"

	"github.com/halbritt/striatum/go/pkg/db"
)

// RFC 0157 (D251) — canonical operator read-surface state projection.
//
// The three primary operator read surfaces for "where is this run and its
// jobs" historically returned the same two facts (run state; per-job state) in
// three structurally different shapes, so a script or AFK agent had to
// special-case each verb:
//
//   - run.summary -> run state nested at `.run.state`; jobs a LIST under
//     `.jobs[].state` (with attempt/role_id detail);
//   - dashboard   -> only `.jobs_by_state` (state->count map), NO top-level
//     run state at all;
//   - status      -> run state inside `.runs[].state` (a list of run objects);
//     `.jobs` a state->count map.
//
// This helper builds ONE additive, lowest-common-denominator projection that
// every surface emits identically, so a consumer can read
// `.state_projection.run_state` + `.state_projection.jobs[].state` uniformly
// without per-verb special-casing. It is STRICTLY ADDITIVE: every existing key
// on every surface is left untouched (run.summary keeps `.run.state` and its
// rich `.jobs[]`; dashboard keeps `.jobs_by_state`; status keeps `.runs[]` and
// `.jobs{}`), so existing AFK scripts and CI assertions do not break. Per RFC
// 0030 §95 an additive result field requires no schema_version bump.
//
// Pinned contract (RFC 0157 Resolution):
//
//	{ "run_state": <string|null>, "jobs": [ { "id": <string>, "state": <string> }, ... ] }
//
// jobs[] is the minimal (id, state) pair ONLY — it deliberately does not carry
// attempt or role_id (those stay on run.summary's own `.jobs[]`). `id` is the
// stable workflow_job_id (the identifier an operator sees across run.summary and
// dashboard), not the internal job_id.
//
// Single-run vs repo-wide: the projection is meaningful only for a single run.
// When invoked without a single run_id (the repo-wide status/dashboard window),
// callers pass an empty runID and get run_state=null with an empty jobs list —
// the projection does not try to flatten a multi-run window into one run_state.

// stateProjectionEmpty is the repo-wide / no-single-run form of the projection:
// a null run_state and an empty jobs array. Kept as a single constructor so the
// repo-wide shape is identical wherever it is emitted.
func stateProjectionEmpty() map[string]any {
	return map[string]any{
		"run_state": nil,
		"jobs":      []map[string]any{},
	}
}

// buildStateProjection assembles the RFC 0157 canonical state projection for a
// single run. When runID is empty (repo-wide scope) it returns the null/empty
// form without touching the database. The run_state is read fresh from
// striatumd.runs so the projection agrees with the run object the surface
// already returns; jobs are the (workflow_job_id, state) pairs for the run.
func buildStateProjection(ctx context.Context, runner db.Runner, repositoryID, runID string) (map[string]any, error) {
	if repositoryID == "" || runID == "" {
		return stateProjectionEmpty(), nil
	}

	runRows, err := collectRows(ctx, runner,
		`SELECT state FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2
		  LIMIT 1`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	var runState any
	if len(runRows) > 0 {
		if s := stringFrom(runRows[0], "state"); s != "" {
			runState = s
		}
	}

	jobRows, err := collectRows(ctx, runner,
		`SELECT workflow_job_id, state
		   FROM striatumd.jobs
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY created_at, workflow_job_id`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	jobs := make([]map[string]any, 0, len(jobRows))
	for _, row := range jobRows {
		jobs = append(jobs, map[string]any{
			"id":    stringFrom(row, "workflow_job_id"),
			"state": stringFrom(row, "state"),
		})
	}

	return map[string]any{
		"run_state": runState,
		"jobs":      jobs,
	}, nil
}
