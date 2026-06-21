package reads

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

type dashboardFakeRunner struct{}

func (dashboardFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("dashboard must be read-only")
}

func (dashboardFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (dashboardFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (dashboardFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("dashboard must not open a transaction")
}

func (dashboardFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM striatumd.runs"):
		// RFC 0157 state_projection reads the run state.
		return dashboardAllRowsFromMaps([]map[string]any{{"state": "running"}}), nil
	case strings.Contains(sql, "FROM striatumd.jobs") && strings.Contains(sql, "workflow_job_id, state"):
		// RFC 0157 state_projection's per-job (id, state) query.
		return dashboardAllRowsFromMaps([]map[string]any{{"workflow_job_id": "wj_1", "state": "queued"}}), nil
	case strings.Contains(sql, "FROM striatumd.jobs"):
		return dashboardAllRowsFromMaps([]map[string]any{{"state": "queued", "c": int64(1)}}), nil
	case strings.Contains(sql, "FROM striatumd.verdicts"):
		return dashboardAllRowsFromMaps(nil), nil
	case strings.Contains(sql, "FROM striatumd.blockers"):
		return dashboardAllRowsFromMaps(nil), nil
	case strings.Contains(sql, "FROM striatumd.sessions s"):
		if strings.Contains(sql, "s.lane_attestation") || strings.Contains(sql, "state, lane_attestation") {
			return nil, errors.New("dashboard selected nonexistent sessions.lane_attestation")
		}
		return dashboardAllRowsFromMaps([]map[string]any{{
			"session_id":              "sess_1",
			"role_id":                 "worker",
			"lane_id":                 "codex",
			"state":                   "open",
			"lane_attestation":        "unattested",
			"lane_attestation_reason": "no_attached_supervisor",
		}}), nil
	case strings.Contains(sql, "FROM striatumd.artifacts"):
		return dashboardAllRowsFromMaps([]map[string]any{
			provenanceArtifactRow("art_1", "sess_1", "author: operator", nil),
		}), nil
	case strings.Contains(sql, "FROM striatumd.events"):
		return dashboardAllRowsFromMaps(nil), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}

func TestHandleDashboardDerivesUnattachedSessionAttestation(t *testing.T) {
	result, err := HandleDashboard(context.Background(), dashboardFakeRunner{}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a", "run_id": "run_a"},
	})
	if err != nil {
		t.Fatalf("HandleDashboard: %v", err)
	}

	sessions, ok := result["sessions"].([]map[string]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("sessions = %#v", result["sessions"])
	}
	session := sessions[0]
	if session["session_id"] != "sess_1" || session["lane_attestation"] != "unattested" {
		t.Fatalf("session attestation = %#v", session)
	}
	if session["lane_attestation_reason"] != "no_attached_supervisor" {
		t.Fatalf("session attestation reason = %#v", session)
	}
	counts := result["artifact_provenance_counts"].(map[string]int)
	if counts[artifactProvenanceUnattestedSession] != 1 {
		t.Fatalf("artifact provenance counts = %#v", counts)
	}
}
