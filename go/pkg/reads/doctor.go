package reads

import (
	"context"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	gosupervisor "github.com/halbritt/striatum/go/pkg/supervisor"
)

// HandleDoctor mirrors reads/doctor.py. Returns a flat health report of
// the running daemon-pg state: schema version, append-only invariants
// detected via probe queries, stale-lease + waiting-human counts.
func HandleDoctor(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, _ := envelope.Params["repository_id"].(string)
	problems := []string{}

	schemaVersion, err := db.ReadSchemaVersion(ctx, runner)
	if err != nil {
		problems = append(problems, "schema_meta.read_failed: "+err.Error())
	}

	staleLeases := 0
	if repositoryID != "" {
		// #45: an expired lease row persists forever, so counting every
		// `state = 'expired'` lease keeps reporting stale leases long after the
		// work was recovered/completed. A lease is only genuinely stale when it
		// is still a job's CURRENT lease AND that job is still actionable
		// (claimed/running/stale_lease) — matching the authoritative predicate in
		// status.go. Recovery or completion swaps current_lease_id / advances the
		// job state, which makes the count drop as expected.
		rows, err := collectRows(ctx, runner,
			`SELECT COUNT(*) AS c
			   FROM striatumd.jobs j
			   JOIN striatumd.leases l
			     ON l.repository_id = j.repository_id
			    AND l.lease_id = j.current_lease_id
			  WHERE j.repository_id = $1
			    AND l.state = 'expired'
			    AND j.state IN ('claimed', 'running', 'stale_lease')`,
			repositoryID,
		)
		if err == nil && len(rows) > 0 {
			if c, ok := rows[0]["c"]; ok {
				switch v := c.(type) {
				case int64:
					staleLeases = int(v)
				case int:
					staleLeases = v
				case float64:
					staleLeases = int(v)
				}
			}
		}
	}

	waitingHuman := 0
	if repositoryID != "" {
		rows, err := collectRows(ctx, runner,
			`SELECT COUNT(*) AS c FROM striatumd.runs
			  WHERE repository_id = $1 AND state = 'waiting_human'`,
			repositoryID,
		)
		if err == nil && len(rows) > 0 {
			if c, ok := rows[0]["c"]; ok {
				switch v := c.(type) {
				case int64:
					waitingHuman = int(v)
				case int:
					waitingHuman = v
				case float64:
					waitingHuman = int(v)
				}
			}
		}
	}
	supervisorLiveness := []map[string]any{}
	if repositoryID != "" {
		if rows, err := reattachStatusRows(ctx, runner, repositoryID, "", "", ""); err == nil {
			for _, row := range rows {
				view := reattachStatusView(row)
				class := superviseString(view["lane_liveness_class"])
				if !strings.HasPrefix(class, "tmux_") {
					continue
				}
				item := map[string]any{
					"supervisor_id": view["supervisor_id"],
					"session_id":    view["session_id"],
					"class":         class,
					"state":         view["reattach_state"],
					"reason":        view["reattach_reason"],
				}
				supervisorLiveness = append(supervisorLiveness, item)
				if view["reattach_state"] != "terminal" && class != string(gosupervisor.TmuxLivenessOK) && class != string(gosupervisor.TmuxLivenessUnavailable) {
					problems = append(problems, "supervisor_liveness."+superviseString(view["supervisor_id"])+": "+class)
				}
			}
		}
	}

	return map[string]any{
		"ok":             len(problems) == 0,
		"schema_version": schemaVersion,
		"stale_leases":   staleLeases,
		"waiting_human":  waitingHuman,
		"supervisors":    supervisorLiveness,
		"problems":       problems,
		"blob":           blobDoctorBlock(ctx, runner, repositoryID),
	}, nil
}
