package reads

import (
	"context"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

func seedSegmentRepo(t *testing.T, ctx context.Context, runner db.Runner, repoID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repositories (
		  repository_id, repo_identity, repo_root, state_db_path, display_name,
		  registered_at, last_schema_version, state
		) VALUES ($1,$2,$3,$4,'repo',$5,14,'active')`,
		repoID, "ident_"+repoID, "/tmp/"+repoID, "/tmp/"+repoID+"/.striatum", now,
	); err != nil {
		t.Fatalf("insert repository: %v", err)
	}
}

// insertSealedSegment inserts a row already in the 'sealed' state. The migration
// trigger only freezes UPDATEs of a sealed row, so a direct INSERT of a sealed
// segment is permitted (this is how the test fabricates seam shapes the doctor
// check then evaluates).
func insertSealedSegment(t *testing.T, ctx context.Context, runner db.Runner, repoID string, id int64,
	firstHash, lastHash string, prevID *int64, prevLastHash *string, nextResumeHash *string) {
	t.Helper()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(
		  repository_id, segment_id, state, retention_state,
		  first_event_id, last_event_id, first_hash, last_hash,
		  previous_segment_id, previous_segment_last_hash, next_segment_first_previous_hash
		) VALUES ($1,$2,'sealed','active',$3,$4,$5,$6,$7,$8,$9)`,
		repoID, id, id*10, id*10+5, firstHash, lastHash, prevID, prevLastHash, nextResumeHash,
	); err != nil {
		t.Fatalf("insert sealed segment %d: %v", id, err)
	}
}

func strp(s string) *string { return &s }
func i64p(i int64) *int64    { return &i }

func TestDoctorEventChainSegmentSeamsGreenWhenChained(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedSegmentRepo(t, ctx, pool.Runner, "repo_doc_a")

	// Segment 1: sealed, no predecessor, resumes at hash "h1".
	insertSealedSegment(t, ctx, pool.Runner, "repo_doc_a", 1, "f1", "h1", nil, nil, strp("h1"))
	// Segment 2: sealed, predecessor 1, seam hash "h1" matching seg1.last_hash and
	// seg1.next_segment_first_previous_hash.
	insertSealedSegment(t, ctx, pool.Runner, "repo_doc_a", 2, "f2", "h2", i64p(1), strp("h1"), strp("h2"))

	block, problems, records := doctorEventChainSegmentSeams(ctx, pool.Runner, "repo_doc_a")
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got %v (records=%v)", problems, records)
	}
	if block["sealed_segments"].(int) != 2 {
		t.Fatalf("sealed_segments = %v, want 2", block["sealed_segments"])
	}
}

func TestDoctorEventChainSegmentSeamsRedOnHashMismatch(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedSegmentRepo(t, ctx, pool.Runner, "repo_doc_b")

	insertSealedSegment(t, ctx, pool.Runner, "repo_doc_b", 1, "f1", "h1", nil, nil, strp("h1"))
	// Segment 2 names predecessor 1 but its previous_segment_last_hash is WRONG.
	insertSealedSegment(t, ctx, pool.Runner, "repo_doc_b", 2, "f2", "h2", i64p(1), strp("WRONG"), strp("h2"))

	_, problems, records := doctorEventChainSegmentSeams(ctx, pool.Runner, "repo_doc_b")
	if len(problems) != 1 || problems[0] != "event_chain_segment_seam_unproven" {
		t.Fatalf("expected one seam_unproven problem, got %v", problems)
	}
	if records[0]["reason"] != "seam_hash_mismatch" {
		t.Fatalf("reason = %v, want seam_hash_mismatch", records[0]["reason"])
	}
}

func TestDoctorEventChainSegmentSeamsRedOnIncompleteBoundary(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedSegmentRepo(t, ctx, pool.Runner, "repo_doc_c")

	// A sealed segment with a NULL last_hash cannot witness its own range.
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(
		  repository_id, segment_id, state, retention_state, first_event_id, last_event_id, first_hash, last_hash
		) VALUES ($1,1,'sealed','active',10,15,'f1',NULL)`, "repo_doc_c"); err != nil {
		t.Fatalf("insert incomplete sealed segment: %v", err)
	}

	_, problems, records := doctorEventChainSegmentSeams(ctx, pool.Runner, "repo_doc_c")
	if len(problems) != 1 {
		t.Fatalf("expected one problem, got %v", problems)
	}
	if records[0]["reason"] != "seal_boundary_incomplete" {
		t.Fatalf("reason = %v, want seal_boundary_incomplete", records[0]["reason"])
	}
}

func TestDoctorEventChainSegmentSeamsSkipsWhenNoSeals(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedSegmentRepo(t, ctx, pool.Runner, "repo_doc_d")
	// Only an OPEN segment exists: nothing sealed to prove, no problems.
	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(repository_id, segment_id, state, retention_state)
		VALUES ($1,1,'open','active')`, "repo_doc_d"); err != nil {
		t.Fatalf("insert open segment: %v", err)
	}
	block, problems, _ := doctorEventChainSegmentSeams(ctx, pool.Runner, "repo_doc_d")
	if len(problems) != 0 {
		t.Fatalf("expected no problems on open-only repo, got %v", problems)
	}
	if block["sealed_segments"].(int) != 0 {
		t.Fatalf("sealed_segments = %v, want 0", block["sealed_segments"])
	}
}
