package mutations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// seedEventChainRepo inserts a repository and a contiguous run of anchored
// events (each with a non-null row_hash) plus the repo_event_chain_heads pointer,
// returning the repository id and the (event_id, row_hash) of every seeded event
// in order. It mirrors what append_event writes (RFC 0136 P1 seals over exactly
// this shape).
func seedEventChainRepo(t *testing.T, ctx context.Context, runner db.Runner, repoID string, count int) []struct {
	EventID int64
	Hash    string
} {
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
	out := make([]struct {
		EventID int64
		Hash    string
	}, 0, count)
	var prevHash *string
	for i := 1; i <= count; i++ {
		hash := repoID + "-hash-" + time.Duration(i).String()
		eventID := int64(i)
		if err := runner.Exec(ctx, `
			INSERT INTO striatumd.events (
			  repository_id, event_id, event_type, payload_json, created_at,
			  previous_hash, row_hash
			) VALUES ($1,$2,'state.transition','{}'::jsonb,$3,$4,$5)`,
			repoID, eventID, now.Add(time.Duration(i)*time.Second), prevHash, hash,
		); err != nil {
			t.Fatalf("insert event %d: %v", eventID, err)
		}
		ph := hash
		prevHash = &ph
		out = append(out, struct {
			EventID int64
			Hash    string
		}{eventID, hash})
	}
	last := out[len(out)-1]
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.repo_event_chain_heads(
		  repository_id, last_event_id, last_hash, updated_at
		) VALUES ($1,$2,$3,now())`,
		repoID, last.EventID, last.Hash,
	); err != nil {
		t.Fatalf("insert chain head: %v", err)
	}
	return out
}

func readEventSegment(t *testing.T, ctx context.Context, runner db.Runner, repoID string, segmentID int64) EventChainSegment {
	t.Helper()
	seg := EventChainSegment{RepositoryID: repoID, SegmentID: segmentID}
	err := runner.QueryRow(ctx, `
		SELECT state, retention_state, first_event_id, last_event_id, first_hash,
		       last_hash, previous_segment_id, previous_segment_last_hash,
		       next_segment_first_previous_hash
		  FROM striatumd.event_chain_segments
		 WHERE repository_id = $1 AND segment_id = $2`,
		repoID, segmentID,
	).Scan(&seg.State, &seg.RetentionState, &seg.FirstEventID, &seg.LastEventID,
		&seg.FirstHash, &seg.LastHash, &seg.PreviousSegmentID,
		&seg.PreviousSegmentLastHash, &seg.NextSegmentFirstPreviousHash)
	if err != nil {
		t.Fatalf("read segment %d: %v", segmentID, err)
	}
	return seg
}

func mustInt64(t *testing.T, p *int64) int64 {
	t.Helper()
	if p == nil {
		t.Fatal("expected non-nil int64")
	}
	return *p
}

func mustStr(t *testing.T, p *string) string {
	t.Helper()
	if p == nil {
		t.Fatal("expected non-nil string")
	}
	return *p
}

// TestSealEventChainSegmentSealsAndOpensSuccessor is the green path: sealing the
// open segment of a repo with anchored events records the first/last boundaries
// + hashes and opens a chained successor.
func TestSealEventChainSegmentSealsAndOpensSuccessor(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	events := seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_a", 5)

	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	sealed, err := SealEventChainSegment(ctx, tx, "repo_seg_a")
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if sealed.SegmentID != 1 {
		t.Fatalf("sealed segment id = %d, want 1", sealed.SegmentID)
	}

	seg1 := readEventSegment(t, ctx, pool.Runner, "repo_seg_a", 1)
	if seg1.State != "sealed" {
		t.Fatalf("segment 1 state = %q, want sealed", seg1.State)
	}
	if got := mustInt64(t, seg1.FirstEventID); got != events[0].EventID {
		t.Fatalf("segment 1 first_event_id = %d, want %d", got, events[0].EventID)
	}
	if got := mustInt64(t, seg1.LastEventID); got != events[4].EventID {
		t.Fatalf("segment 1 last_event_id = %d, want %d", got, events[4].EventID)
	}
	if got := mustStr(t, seg1.FirstHash); got != events[0].Hash {
		t.Fatalf("segment 1 first_hash = %q, want %q", got, events[0].Hash)
	}
	if got := mustStr(t, seg1.LastHash); got != events[4].Hash {
		t.Fatalf("segment 1 last_hash = %q, want %q", got, events[4].Hash)
	}
	// The cross-segment witness: the chain resumes at the sealed last hash.
	if got := mustStr(t, seg1.NextSegmentFirstPreviousHash); got != events[4].Hash {
		t.Fatalf("segment 1 next_segment_first_previous_hash = %q, want %q", got, events[4].Hash)
	}

	// The successor open segment is chained back to segment 1.
	seg2 := readEventSegment(t, ctx, pool.Runner, "repo_seg_a", 2)
	if seg2.State != "open" {
		t.Fatalf("segment 2 state = %q, want open", seg2.State)
	}
	if got := mustInt64(t, seg2.PreviousSegmentID); got != 1 {
		t.Fatalf("segment 2 previous_segment_id = %d, want 1", got)
	}
	if got := mustStr(t, seg2.PreviousSegmentLastHash); got != events[4].Hash {
		t.Fatalf("segment 2 previous_segment_last_hash = %q, want %q (seam continuity)", got, events[4].Hash)
	}
}

// TestSealEventChainSegmentChainsAcrossTwoSeals proves a second seal continues
// from the first: its first boundary is the event AFTER the first seal's last,
// and its previous_segment witnesses link to segment 1.
func TestSealEventChainSegmentChainsAcrossTwoSeals(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	events := seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_b", 3)

	// First seal over events 1..3.
	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	if _, err := SealEventChainSegment(ctx, tx, "repo_seg_b"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seal1: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit1: %v", err)
	}

	// Append two more events and advance the head, then seal again.
	now := time.Now().UTC()
	moreHashes := []string{"repo_seg_b-hash-4", "repo_seg_b-hash-5"}
	prev := events[2].Hash
	for i, h := range moreHashes {
		eid := int64(4 + i)
		if err := pool.Runner.Exec(ctx, `
			INSERT INTO striatumd.events (
			  repository_id, event_id, event_type, payload_json, created_at, previous_hash, row_hash
			) VALUES ($1,$2,'state.transition','{}'::jsonb,$3,$4,$5)`,
			"repo_seg_b", eid, now.Add(time.Duration(eid)*time.Second), prev, h,
		); err != nil {
			t.Fatalf("insert later event %d: %v", eid, err)
		}
		prev = h
	}
	if err := pool.Runner.Exec(ctx, `
		UPDATE striatumd.repo_event_chain_heads SET last_event_id=5, last_hash=$2, updated_at=now()
		 WHERE repository_id=$1`, "repo_seg_b", moreHashes[1]); err != nil {
		t.Fatalf("advance head: %v", err)
	}

	tx2, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	sealed2, err := SealEventChainSegment(ctx, tx2, "repo_seg_b")
	if err != nil {
		_ = tx2.Rollback(ctx)
		t.Fatalf("seal2: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit2: %v", err)
	}
	if sealed2.SegmentID != 2 {
		t.Fatalf("second sealed segment id = %d, want 2", sealed2.SegmentID)
	}

	seg2 := readEventSegment(t, ctx, pool.Runner, "repo_seg_b", 2)
	if got := mustInt64(t, seg2.FirstEventID); got != 4 {
		t.Fatalf("segment 2 first_event_id = %d, want 4 (event after first seal)", got)
	}
	if got := mustInt64(t, seg2.LastEventID); got != 5 {
		t.Fatalf("segment 2 last_event_id = %d, want 5", got)
	}
	if got := mustStr(t, seg2.PreviousSegmentLastHash); got != events[2].Hash {
		t.Fatalf("segment 2 previous_segment_last_hash = %q, want %q (seam to segment 1)", got, events[2].Hash)
	}
	// A third (open) successor exists.
	seg3 := readEventSegment(t, ctx, pool.Runner, "repo_seg_b", 3)
	if seg3.State != "open" {
		t.Fatalf("segment 3 state = %q, want open", seg3.State)
	}
}

// TestSealEventChainSegmentEmptyRangeIsNoOp proves a re-seal with no new events
// returns ErrEventChainSegmentNoEvents and writes nothing new.
func TestSealEventChainSegmentEmptyRangeIsNoOp(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_c", 2)

	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := SealEventChainSegment(ctx, tx, "repo_seg_c"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("first seal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Re-seal with no new events: head still points at event 2, segment 1 sealed
	// through event 2, so the empty-range guard fires.
	tx2, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	_, err = SealEventChainSegment(ctx, tx2, "repo_seg_c")
	_ = tx2.Rollback(ctx)
	if !errors.Is(err, ErrEventChainSegmentNoEvents) {
		t.Fatalf("re-seal error = %v, want ErrEventChainSegmentNoEvents", err)
	}
}

// TestSealEventChainSegmentRefusesUnanchoredBoundary proves the Go referential-
// integrity check (D215): if the chain head names an event whose stored row_hash
// does not match (the boundary is missing/unanchored), the seal refuses rather
// than recording a false boundary. There is no SQL FK doing this.
func TestSealEventChainSegmentRefusesUnanchoredBoundary(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_d", 3)

	// Corrupt the head so its last_hash disagrees with the event's stored row_hash.
	if err := pool.Runner.Exec(ctx, `
		UPDATE striatumd.repo_event_chain_heads SET last_hash='not-the-real-hash'
		 WHERE repository_id=$1`, "repo_seg_d"); err != nil {
		t.Fatalf("corrupt head: %v", err)
	}

	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_, err = SealEventChainSegment(ctx, tx, "repo_seg_d")
	_ = tx.Rollback(ctx)
	if !errors.Is(err, ErrEventChainSegmentBoundaryMissing) {
		t.Fatalf("seal error = %v, want ErrEventChainSegmentBoundaryMissing", err)
	}
}

// TestSealedEventSegmentIsAppendOnly proves the migration's append-only triggers:
// a sealed segment refuses UPDATE, and no segment may be DELETEd.
func TestSealedEventSegmentIsAppendOnly(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_e", 2)

	tx, err := pool.Runner.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := SealEventChainSegment(ctx, tx, "repo_seg_e"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("seal: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// UPDATE of the sealed segment is refused by refuse_sealed_event_segment_change.
	err = pool.Runner.Exec(ctx, `
		UPDATE striatumd.event_chain_segments SET retention_state='purged'
		 WHERE repository_id=$1 AND segment_id=1`, "repo_seg_e")
	if err == nil {
		t.Fatal("expected UPDATE of sealed segment to be refused")
	}
	// DELETE of any segment is refused by event_chain_segments_no_delete.
	err = pool.Runner.Exec(ctx, `
		DELETE FROM striatumd.event_chain_segments WHERE repository_id=$1 AND segment_id=2`, "repo_seg_e")
	if err == nil {
		t.Fatal("expected DELETE of segment to be refused")
	}
}

// TestOnlyOneOpenSegmentPerRepo proves the partial unique index: a second open
// segment for the same repository is rejected.
func TestOnlyOneOpenSegmentPerRepo(t *testing.T) {
	pool := pgtest.Pool(t)
	ctx := context.Background()
	seedEventChainRepo(t, ctx, pool.Runner, "repo_seg_f", 1)

	if err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(repository_id, segment_id, state, retention_state)
		VALUES ($1, 1, 'open', 'active')`, "repo_seg_f"); err != nil {
		t.Fatalf("insert first open segment: %v", err)
	}
	err := pool.Runner.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(repository_id, segment_id, state, retention_state)
		VALUES ($1, 2, 'open', 'active')`, "repo_seg_f")
	if err == nil {
		t.Fatal("expected a second open segment to violate uq_event_chain_segments_one_open_per_repo")
	}
}
