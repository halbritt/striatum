package reads

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleTrajectoryExport projects ordered trajectory records for a run.
func HandleTrajectoryExport(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "trajectory.export requires run_id", nil)
	}
	profile := stringParam(envelope, "profile")
	if profile == "" {
		profile = "dialogue"
	}
	if profile != "dialogue" && profile != "provenance" {
		return nil, rpc.NewError("schema_invalid", "invalid profile: "+profile, nil)
	}

	records, err := fetchTrajectory(ctx, runner, repositoryID, runID, profile, 0)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"repository_id": repositoryID,
		"run_id":        runID,
		"profile":       profile,
		"records":       records,
	}, nil
}

// HandleTrajectoryWatch tails trajectory records for a run since a sequence.
func HandleTrajectoryWatch(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "trajectory.watch requires run_id", nil)
	}
	profile := stringParam(envelope, "profile")
	if profile == "" {
		profile = "dialogue"
	}
	sinceSeq := int64(intFrom(envelope.Params, "since_seq"))

	records, err := fetchTrajectory(ctx, runner, repositoryID, runID, profile, sinceSeq)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"repository_id": repositoryID,
		"run_id":        runID,
		"profile":       profile,
		"since_seq":     sinceSeq,
		"records":       records,
	}, nil
}

func fetchTrajectory(ctx context.Context, runner db.Runner, repositoryID, runID, profile string, sinceSeq int64) ([]map[string]any, error) {
	// We build a UNION query to pull from messages, events, artifacts, verdicts, and blockers.
	// Each branch is filtered by profile.

	var queries []string
	var args []any
	args = append(args, repositoryID, runID, sinceSeq)

	// Per the converged design, the trajectory is a READ MODEL over existing
	// daemon-owned records: ordering is DERIVED here (ROW_NUMBER over created_at,
	// then a stable source class + per-row primary key as tie-breakers) rather
	// than read from a stored run_event_seq column. No existing table is altered;
	// the daemon adds no new authority. Projected rows carry curated fields only
	// (D028: never provider stdout/stderr). The `since_seq` cursor filters the
	// derived sequence in the outer query.

	// 1. Messages (Dialogue + Provenance)
	messageFilter := "'agent_message', 'coordinator_message'"
	if profile == "provenance" {
		messageFilter = "'agent_message', 'coordinator_message', 'work', 'human_checkpoint', 'commit_request'"
	}

	queries = append(queries, fmt.Sprintf(`
		SELECT created_at AS ts, 1 AS src, message_id::text AS tiebreak, kind,
		       target_session_id AS session_id, target_role_id AS role_id, target_lane_id AS lane_id,
		       payload_json->>'parent_message_id' AS parent_message_id,
		       payload_json AS body, NULL::jsonb AS refs
		  FROM striatumd.queue_messages
		 WHERE repository_id = $1 AND run_id = $2 AND kind IN (%s)`, messageFilter))

	// 2. Events (Provenance only)
	if profile == "provenance" {
		queries = append(queries, `
			SELECT created_at AS ts, 2 AS src, event_id::text AS tiebreak, event_type AS kind,
			       actor_session_id AS session_id, NULL AS role_id, NULL AS lane_id,
			       NULL AS parent_message_id, payload_json AS body, NULL::jsonb AS refs
			  FROM striatumd.events
			 WHERE repository_id = $1 AND run_id = $2`)
	}

	// 3. Artifacts (Dialogue + Provenance) projected as 'artifact_published'.
	queries = append(queries, `
		SELECT a.created_at AS ts, 3 AS src, a.artifact_id::text AS tiebreak, 'artifact_published' AS kind,
		       a.session_id, NULL AS role_id, NULL AS lane_id, NULL AS parent_message_id,
		       jsonb_build_object('artifact_id', a.artifact_id, 'logical_name', a.logical_name, 'kind', a.artifact_kind, 'path', a.repo_path, 'placement', `+artifactPlacementExpression(ctx, runner, "a")+`) AS body,
		       NULL::jsonb AS refs
		  FROM striatumd.artifacts a
		 WHERE a.repository_id = $1 AND a.run_id = $2`)

	// 4. Verdicts (Provenance only)
	if profile == "provenance" {
		queries = append(queries, `
			SELECT created_at AS ts, 4 AS src, verdict_id::text AS tiebreak, 'verdict' AS kind,
			       session_id, NULL AS role_id, NULL AS lane_id, NULL AS parent_message_id,
			       jsonb_build_object('verdict_id', verdict_id, 'verdict', verdict, 'rationale', rationale, 'posture', posture) AS body,
			       NULL::jsonb AS refs
			  FROM striatumd.verdicts
			 WHERE repository_id = $1 AND run_id = $2`)
	}

	// 5. Blockers (Provenance only)
	if profile == "provenance" {
		queries = append(queries, `
			SELECT created_at AS ts, 5 AS src, blocker_id::text AS tiebreak, 'blocker' AS kind,
			       session_id, NULL AS role_id, NULL AS lane_id, NULL AS parent_message_id,
			       jsonb_build_object('blocker_id', blocker_id, 'severity', severity, 'kind', blocker_kind, 'description', description, 'state', state) AS body,
			       NULL::jsonb AS refs
			  FROM striatumd.blockers
			 WHERE repository_id = $1 AND run_id = $2`)
	}

	union := ""
	for i, q := range queries {
		if i > 0 {
			union += " UNION ALL "
		}
		union += q
	}
	fullQuery := fmt.Sprintf(`
		SELECT kind, ts, session_id, role_id, lane_id, parent_message_id, body, refs, seq
		  FROM (
		    SELECT sub.*, ROW_NUMBER() OVER (ORDER BY ts ASC, src ASC, tiebreak ASC) AS seq
		      FROM ( %s ) sub
		  ) numbered
		 WHERE seq > $3
		 ORDER BY seq ASC`, union)

	rows, err := collectRows(ctx, runner, fullQuery, args...)
	if err != nil {
		return nil, err
	}

	// Curate bodies to ensure D028 compliance.
	for _, row := range rows {
		kind, _ := row["kind"].(string)
		body, _ := row["body"].(map[string]any)
		if kind == "agent_message" || kind == "coordinator_message" {
			// Surface the agent-authored content the bus carried into "text",
			// never raw provider output (D028). work.send_message stores
			// {kind, body}; body may be a string, a {text: ...} object, or the
			// message may carry a top-level "text" field — handle each shape.
			curated := map[string]any{}
			if mk, ok := body["kind"].(string); ok {
				curated["message_kind"] = mk
			}
			switch inner := body["body"].(type) {
			case string:
				curated["text"] = inner
			case map[string]any:
				if t, ok := inner["text"].(string); ok {
					curated["text"] = t
				} else {
					curated["text"] = inner
				}
			default:
				if t, ok := body["text"].(string); ok {
					curated["text"] = t
				}
			}
			if topic, ok := body["topic"].(string); ok {
				curated["topic"] = topic
			}
			// RFC 0082: interrogation turns ride the message bus as curated
			// agent messages. Surface their correlation identifiers (still
			// authored fields only, D028) so the dialogue reproduces the thread.
			if interrogationID, ok := body["interrogation_id"].(string); ok && interrogationID != "" {
				curated["interrogation_id"] = interrogationID
				if turn, ok := body["turn"]; ok {
					curated["turn"] = turn
				}
				if turnIndex, ok := body["turn_index"]; ok {
					curated["turn_index"] = turnIndex
				}
			}
			row["body"] = curated
		}
	}

	return rows, nil
}
