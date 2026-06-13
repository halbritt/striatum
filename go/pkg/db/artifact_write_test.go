package db

import (
	"context"
	"strings"
	"testing"
)

// recordingArtifactTx captures the SQL of the single write AppendArtifactInTx
// issues, and which method carried it, so the routing decision can be asserted
// without a database.
type recordingArtifactTx struct {
	execSQL   string
	scalarSQL string
}

func (t *recordingArtifactTx) Exec(_ context.Context, sql string, _ ...any) error {
	t.execSQL = sql
	return nil
}
func (t *recordingArtifactTx) QueryRow(context.Context, string, ...any) Row { return nil }
func (t *recordingArtifactTx) QueryScalar(_ context.Context, sql string, _ ...any) (string, error) {
	if strings.Contains(sql, "information_schema.columns") {
		return "false", nil
	}
	t.scalarSQL = sql
	return "", nil
}
func (t *recordingArtifactTx) Commit(context.Context) error   { return nil }
func (t *recordingArtifactTx) Rollback(context.Context) error { return nil }

// TestAppendArtifactRoutesByPhase is the RFC 0110 §7 routing gate: below
// audit_artifacts the artifact write is a direct INSERT; at or above it routes
// through the owner-owned SECURITY DEFINER append_artifact_row.
func TestAppendArtifactRoutesByPhase(t *testing.T) {
	t.Cleanup(func() { SetActiveWriteBoundary(PhaseNone) })
	row := ArtifactRow{RepositoryID: "repo", ArtifactID: "art", RunID: "run", LogicalName: "log"}

	for _, tc := range []struct {
		phase  WriteBoundaryPhase
		wantSD bool
	}{
		{PhaseNone, false},
		{PhaseAuditOnly, false},
		{PhaseAuditArtifacts, true},
		{PhaseFull, true},
	} {
		SetActiveWriteBoundary(tc.phase)
		tx := &recordingArtifactTx{}
		if err := AppendArtifactInTx(context.Background(), tx, row); err != nil {
			t.Fatalf("phase %s: %v", tc.phase, err)
		}
		if tc.wantSD {
			if !strings.Contains(tx.scalarSQL, "striatumd.append_artifact_row") {
				t.Fatalf("phase %s: expected SD routing, got exec=%q scalar=%q", tc.phase, tx.execSQL, tx.scalarSQL)
			}
			if tx.execSQL != "" {
				t.Fatalf("phase %s: SD routing must not also direct-INSERT (exec=%q)", tc.phase, tx.execSQL)
			}
		} else {
			if !strings.Contains(tx.execSQL, "INSERT INTO striatumd.artifacts") {
				t.Fatalf("phase %s: expected direct INSERT, got exec=%q scalar=%q", tc.phase, tx.execSQL, tx.scalarSQL)
			}
			if tx.scalarSQL != "" {
				t.Fatalf("phase %s: pre-P1 must not call the SD function (scalar=%q)", tc.phase, tx.scalarSQL)
			}
		}
	}
}

func TestAppendArtifactDirectIncludesPlacementWhenColumnExists(t *testing.T) {
	tx := &recordingPlacementArtifactTx{placementColumnPresent: true}
	row := ArtifactRow{
		RepositoryID: "repo", ArtifactID: "art", RunID: "run", LogicalName: "log",
		Placement: "git_publication",
	}
	if err := appendArtifactRowDirect(context.Background(), tx, row); err != nil {
		t.Fatalf("appendArtifactRowDirect: %v", err)
	}
	if !strings.Contains(tx.execSQL, "placement") || len(tx.execArgs) != 18 || tx.execArgs[17] != "git_publication" {
		t.Fatalf("direct placement insert sql=%q args=%#v", tx.execSQL, tx.execArgs)
	}
}

func TestAppendArtifactSDIncludesPlacementWhenOverloadExists(t *testing.T) {
	tx := &recordingPlacementArtifactTx{placementOverloadPresent: true}
	row := ArtifactRow{
		RepositoryID: "repo", ArtifactID: "art", RunID: "run", LogicalName: "log",
		Placement: "blob_exhaust",
	}
	if err := appendArtifactRowSD(context.Background(), tx, row); err != nil {
		t.Fatalf("appendArtifactRowSD: %v", err)
	}
	if !strings.Contains(tx.scalarSQL, "$18") || len(tx.scalarArgs) != 18 || tx.scalarArgs[17] != "blob_exhaust" {
		t.Fatalf("sd placement call sql=%q args=%#v", tx.scalarSQL, tx.scalarArgs)
	}
}

type recordingPlacementArtifactTx struct {
	recordingArtifactTx
	placementColumnPresent   bool
	placementOverloadPresent bool
	execArgs                 []any
	scalarArgs               []any
}

func (t *recordingPlacementArtifactTx) Exec(_ context.Context, sql string, args ...any) error {
	t.execSQL = sql
	t.execArgs = args
	return nil
}

func (t *recordingPlacementArtifactTx) QueryScalar(_ context.Context, sql string, args ...any) (string, error) {
	switch {
	case strings.Contains(sql, "information_schema.columns"):
		if t.placementColumnPresent {
			return "true", nil
		}
		return "false", nil
	case strings.Contains(sql, "to_regprocedure"):
		if t.placementOverloadPresent {
			return "true", nil
		}
		return "false", nil
	default:
		t.scalarSQL = sql
		t.scalarArgs = args
		return "", nil
	}
}
