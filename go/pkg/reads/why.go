package reads

import (
	"context"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleWhy mirrors reads/why.py. Given a target_id (job_id, session_id,
// run_id, artifact_id, or message_id), return the most recent events
// that touched the target. Cross-references the events table by any of
// those columns to give an operator the "why is this thing in this
// state" trace.
func HandleWhy(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	targetID := stringParam(envelope, "target_id")
	if targetID == "" {
		return nil, rpc.NewError("schema_invalid", "why requires a target_id positional argument — a run_id, job_id, session_id, message_id, lease_id, or blocker_id (e.g. `striatum why <run_id>`); list runs with `striatum list runs` and find ids with `striatum status --run-id <id>`", nil)
	}
	events, err := collectRows(ctx, runner,
		`SELECT event_id, run_id, event_type, job_id, message_id, lease_id,
		        created_at, payload_json
		   FROM striatumd.events
		  WHERE repository_id = $1
		    AND ( job_id = $2 OR message_id = $2 OR lease_id = $2
		          OR run_id = $2 OR payload_json::text ILIKE '%' || $2 || '%' )
		  ORDER BY event_id DESC LIMIT 50`,
		repositoryID, targetID,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"target_id": targetID,
		"events":    events,
	}, nil
}
