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

type listArtifactsFakeRunner struct{}

func (listArtifactsFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("list artifacts must be read-only")
}

func (listArtifactsFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (listArtifactsFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (listArtifactsFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("list artifacts must not open a transaction")
}

func (listArtifactsFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, " AND kind =") ||
		strings.Contains(sql, " session_id, kind,") ||
		strings.Contains(sql, " ORDER BY published_at") {
		return nil, errors.New("list artifacts selected legacy artifact column names")
	}
	if !strings.Contains(sql, "artifact_kind AS kind") ||
		!strings.Contains(sql, "repo_path AS path") ||
		!strings.Contains(sql, "author_line AS byline") ||
		!strings.Contains(sql, "created_at AS published_at") ||
		!strings.Contains(sql, "AND a.artifact_kind = $3") ||
		!strings.Contains(sql, "LEFT JOIN striatumd.sessions s") ||
		!strings.Contains(sql, "artifact.auto_finalized") {
		return nil, errors.New("list artifacts did not project current schema columns")
	}
	if len(args) != 3 || args[0] != "repo_a" || args[1] != "run_a" || args[2] != "synthesis" {
		return nil, errors.New("unexpected args")
	}
	return dashboardAllRowsFromMaps([]map[string]any{{
		"artifact_id":    "art_1",
		"run_id":         "run_a",
		"job_id":         "job_a",
		"session_id":     "sess_a",
		"kind":           "synthesis",
		"logical_name":   "summary",
		"path":           "docs/SUMMARY.md",
		"content_sha256": "abc",
		"byline":         "author: tester",
		"published_at":   "2026-05-21T00:00:00Z",
	}}), nil
}

type listWorkflowsFakeRunner struct{}

func (listWorkflowsFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("list workflows must be read-only")
}

func (listWorkflowsFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (listWorkflowsFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (listWorkflowsFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("list workflows must not open a transaction")
}

func (listWorkflowsFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	// #142: workflow_snapshots has content_sha256 / loaded_at, not
	// snapshot_sha256 / captured_at. Reject the legacy projection that 42703'd.
	if strings.Contains(sql, " snapshot_sha256, captured_at") ||
		strings.Contains(sql, "ORDER BY captured_at") {
		return nil, errors.New("list workflows selected legacy snapshot column names")
	}
	if !strings.Contains(sql, "content_sha256 AS snapshot_sha256") ||
		!strings.Contains(sql, "loaded_at AS captured_at") ||
		!strings.Contains(sql, "ORDER BY loaded_at") {
		return nil, errors.New("list workflows did not project current schema columns")
	}
	if len(args) != 1 || args[0] != "repo_a" {
		return nil, errors.New("unexpected args")
	}
	return dashboardAllRowsFromMaps([]map[string]any{{
		"workflow_snapshot_id": "wfs_1",
		"workflow_id":          "demo-flow",
		"workflow_version":     "2026-06-01",
		"snapshot_sha256":      "abc",
		"captured_at":          "2026-06-01T00:00:00Z",
	}}), nil
}

func TestHandleListWorkflowsUsesCurrentSchemaColumnNames(t *testing.T) {
	result, err := HandleListWorkflows(context.Background(), listWorkflowsFakeRunner{}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_a",
		},
	})
	if err != nil {
		t.Fatalf("HandleListWorkflows: %v", err)
	}
	if result["count"] != 1 {
		t.Fatalf("count = %#v", result["count"])
	}
	items := result["items"].([]map[string]any)
	if items[0]["workflow_id"] != "demo-flow" || items[0]["snapshot_sha256"] != "abc" {
		t.Fatalf("workflow row = %#v", items[0])
	}
}

func TestHandleListArtifactsUsesCurrentSchemaColumnNames(t *testing.T) {
	result, err := HandleListArtifacts(context.Background(), listArtifactsFakeRunner{}, rpc.Envelope{
		Params: map[string]any{
			"repository_id": "repo_a",
			"run_id":        "run_a",
			"kind":          "synthesis",
		},
	})
	if err != nil {
		t.Fatalf("HandleListArtifacts: %v", err)
	}
	if result["count"] != 1 {
		t.Fatalf("count = %#v", result["count"])
	}
	items := result["items"].([]map[string]any)
	if items[0]["kind"] != "synthesis" || items[0]["path"] != "docs/SUMMARY.md" || items[0]["placement"] != "blob_exhaust" {
		t.Fatalf("artifact row = %#v", items[0])
	}
}

type listArtifactsProvenanceFakeRunner struct{}

func (listArtifactsProvenanceFakeRunner) Exec(context.Context, string, ...any) error {
	return errors.New("list artifacts must be read-only")
}

func (listArtifactsProvenanceFakeRunner) QueryRow(context.Context, string, ...any) db.Row {
	return dashboardAllFakeRow{}
}

func (listArtifactsProvenanceFakeRunner) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected scalar query")
}

func (listArtifactsProvenanceFakeRunner) BeginTx(context.Context) (db.TxRunner, error) {
	return nil, errors.New("list artifacts must not open a transaction")
}

func (listArtifactsProvenanceFakeRunner) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "LEFT JOIN LATERAL") ||
		!strings.Contains(sql, "supervisor.artifact_observed") ||
		!strings.Contains(sql, "provenance.publish_without_process_execution") {
		return nil, errors.New("list artifacts did not include provenance joins")
	}
	if len(args) != 1 || args[0] != "repo_a" {
		return nil, errors.New("unexpected args")
	}
	return dashboardAllRowsFromMaps([]map[string]any{
		provenanceArtifactRow("art_attested", "sess_attested", "author: reviewer-codex-001", map[string]any{
			"_provenance_supervisor_id":    "sup_1",
			"_provenance_supervisor_state": "stopped",
		}),
		provenanceArtifactRow("art_unattested", "sess_unattested", "author: operator", nil),
		provenanceArtifactRow("art_auto", "sess_auto", "author: operator", map[string]any{
			"_provenance_event_type": artifactEventAutoFinalized,
		}),
		provenanceArtifactRow("art_on_behalf", "sess_override", "author: operator", map[string]any{
			"_provenance_event_type":         artifactEventPublishWithoutProcess,
			"_provenance_event_payload":      map[string]any{"override_rationale": "operator inspected lane output"},
			"_provenance_override_rationale": "operator inspected lane output",
		}),
		provenanceArtifactRow("art_operator_self", "sess_operator", "author: operator-self-declared-codex-driver", map[string]any{
			"_provenance_operator_label": "codex-driver",
		}),
	}), nil
}

func TestHandleListArtifactsAddsProvenanceCategories(t *testing.T) {
	result, err := HandleListArtifacts(context.Background(), listArtifactsProvenanceFakeRunner{}, rpc.Envelope{
		Params: map[string]any{"repository_id": "repo_a"},
	})
	if err != nil {
		t.Fatalf("HandleListArtifacts: %v", err)
	}
	items := result["items"].([]map[string]any)
	got := map[string]string{}
	for _, item := range items {
		got[stringFrom(item, "artifact_id")] = artifactCategoryForTest(t, item)
		if _, exists := item["_provenance_event_type"]; exists {
			t.Fatalf("scratch provenance field leaked into output: %#v", item)
		}
	}
	want := map[string]string{
		"art_attested":      artifactProvenanceAttestedLane,
		"art_unattested":    artifactProvenanceUnattestedSession,
		"art_auto":          artifactProvenanceAutoFinalized,
		"art_on_behalf":     artifactProvenancePublishedOnBehalf,
		"art_operator_self": artifactProvenanceOperatorSelf,
	}
	for artifactID, category := range want {
		if got[artifactID] != category {
			t.Fatalf("%s category = %q, want %q (all categories %#v)", artifactID, got[artifactID], category, got)
		}
	}
}

func provenanceArtifactRow(artifactID, sessionID, byline string, extras map[string]any) map[string]any {
	row := map[string]any{
		"artifact_id":    artifactID,
		"run_id":         "run_a",
		"job_id":         "job_a",
		"session_id":     sessionID,
		"kind":           "finding",
		"logical_name":   artifactID,
		"path":           "docs/" + artifactID + ".md",
		"content_sha256": artifactID + "_sha",
		"byline":         byline,
		"published_at":   "2026-06-01T00:00:00Z",
	}
	for _, key := range []string{
		"_provenance_override_rationale",
		"_provenance_operator_label",
		"_provenance_session_state",
		"_provenance_supervisor_id",
		"_provenance_supervisor_state",
		"_provenance_observed_event_id",
		"_provenance_process_id",
		"_provenance_event_type",
		"_provenance_event_payload",
		"_provenance_recovery_event_type",
		"_provenance_recovery_event_payload",
	} {
		row[key] = nil
	}
	for key, value := range extras {
		row[key] = value
	}
	return row
}

func artifactCategoryForTest(t *testing.T, row map[string]any) string {
	t.Helper()
	provenance, ok := row["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("missing provenance in row %#v", row)
	}
	return stringValue(provenance["category"])
}
