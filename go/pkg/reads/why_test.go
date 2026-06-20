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

// #477: `why <run_id>` for a needs_operator run was empty — the natural
// "what do I do" surface answered with nothing. Assert it now projects the open
// blockers reachable from the target into actionable entries that name the
// escalation_id and the literal resolve command.
func TestHandleWhySurfacesActiveRunBlockers(t *testing.T) {
	ctx := context.Background()
	runID := "run_why_active_blockers"
	result, err := HandleWhy(ctx, whyActiveBlockerFakeRunner{}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_why_active_blockers",
			"target_id":     runID,
		},
	})
	if err != nil {
		t.Fatalf("HandleWhy: %v", err)
	}
	blockers, ok := result["active_blockers"].([]map[string]any)
	if !ok || len(blockers) != 1 {
		t.Fatalf("active_blockers = %#v, want one open blocker", result["active_blockers"])
	}
	alias, ok := result["blockers"].([]map[string]any)
	if !ok || len(alias) != len(blockers) {
		t.Fatalf("blockers alias = %#v, want the active_blockers list", result["blockers"])
	}
	blocker := blockers[0]
	if blocker["blocker_id"] != "blk_why" || blocker["escalation_id"] != "blk_why" {
		t.Fatalf("active blocker ids = %#v, want blk_why", blocker)
	}
	if blocker["resolution_command"] != "striatum escalation resolve blk_why" {
		t.Fatalf("resolution_command = %#v", blocker["resolution_command"])
	}
	if blocker["resolve_command"] != "striatum escalation resolve blk_why --json" {
		t.Fatalf("resolve_command = %#v", blocker["resolve_command"])
	}
}

type whyActiveBlockerFakeRunner struct{}

func (whyActiveBlockerFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("why must be read-only")
}

func (whyActiveBlockerFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (whyActiveBlockerFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (whyActiveBlockerFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("why must not open a transaction")
}

func (whyActiveBlockerFakeRunner) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	switch {
	case strings.Contains(sql, "FROM striatumd.events"):
		return dashboardAllRowsFromMaps(nil), nil
	case strings.Contains(sql, "FROM striatumd.blockers b"):
		return dashboardAllRowsFromMaps([]map[string]any{{
			"blocker_id":      "blk_why",
			"run_id":          "run_why_active_blockers",
			"job_id":          "job_why_blocked",
			"workflow_job_id": "review_final",
			"session_id":      nil,
			"severity":        "blocked",
			"blocker_kind":    "recovery_exhausted",
			"state":           "open",
			"created_at":      "2026-06-20T00:00:00Z",
		}}), nil
	default:
		return nil, errors.New("unexpected query: " + sql)
	}
}
