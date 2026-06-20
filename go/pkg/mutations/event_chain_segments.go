package mutations

import (
	"context"
	"errors"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/jackc/pgx/v5"
)

// RFC 0136 P1 (D242) — chain-segment sealing for the per-repository event chain.
//
// This generalizes the audit_segments "seal + boundary-hash + retention_state"
// model (0001_baseline.sql:68, managed in-DB by append_audit_row) to the event
// chain, so an event partition can be SEALED and proven-continuous BEFORE any
// partition DROP (RFC 0136 P2/P4). Unlike audit_segments (owner-held, single
// global chain) this ledger is the runtime table striatumd.event_chain_segments
// (migration 0041), keyed per repository because the event chain is per-repo.
//
// Per D215 the table carries NO foreign key into the owner-held events table;
// the referential integrity between a sealed segment's boundary event ids/hashes
// and the actual events rows is enforced HERE, in Go, inside the sealing
// transaction — exactly the rule RFC 0136 §"chain-head FK is the sharp edge"
// applies to repo_event_chain_heads in P2. The seal therefore:
//   1. locks the open segment row FOR UPDATE (one open per repo, the seam point),
//   2. reads the boundary event (the chain head, or an explicit last_event_id)
//      and ASSERTS in Go that it exists, has a non-null row_hash, and is >= the
//      segment's first boundary,
//   3. flips the open segment to 'sealed' with its first/last event ids +
//      created_at + row hashes recorded,
//   4. opens the successor segment chained via previous_segment_id /
//      previous_segment_last_hash / next_segment_first_previous_hash so a
//      verifier can prove continuity across the (future) dropped-partition seam
//      WITHOUT the dropped rows.

// ErrEventChainSegmentNoEvents is returned when a seal is requested for a
// repository whose open segment has no events to seal (the chain head does not
// advance past the segment's already-sealed boundary). Sealing an empty range is
// a no-op the caller can ignore, not a corruption.
var ErrEventChainSegmentNoEvents = errors.New("event chain segment has no unsealed events to seal")

// ErrEventChainSegmentBoundaryMissing is the Go-enforced referential-integrity
// failure (D215): the requested boundary event id does not exist for the
// repository, or carries no row hash, so it cannot anchor a sealed segment.
var ErrEventChainSegmentBoundaryMissing = errors.New("event chain segment boundary event is missing or unanchored")

// EventChainSegment is the durable seal record for one sealed (or open) range of
// a repository's event chain. It mirrors the audit_segments columns, keyed per
// repository.
type EventChainSegment struct {
	RepositoryID                 string
	SegmentID                    int64
	State                        string
	RetentionState               string
	FirstEventID                 *int64
	LastEventID                  *int64
	FirstHash                    *string
	LastHash                     *string
	PreviousSegmentID            *int64
	PreviousSegmentLastHash      *string
	NextSegmentFirstPreviousHash *string
}

// eventSegmentRunner is the transactional surface the seal needs: a row read
// (FOR UPDATE locking), a scalar read, and an exec. db.TxRunner satisfies it,
// and so do the pgtest rollback runner and the unit-test fakes.
type eventSegmentRunner interface {
	Exec(ctx context.Context, sql string, args ...any) error
	QueryRow(ctx context.Context, sql string, args ...any) db.Row
}

// SealEventChainSegment seals the open event-chain segment for a repository up to
// the repository's current chain head (the highest anchored event), then opens a
// chained successor segment. It is idempotent on an empty range: if the chain
// head has not advanced past the open segment's already-recorded last boundary it
// returns ErrEventChainSegmentNoEvents and writes nothing.
//
// It must run inside a transaction (the caller owns commit/rollback) so the
// close-then-open is atomic and the FOR UPDATE lock on the open segment
// serializes concurrent sealers for the same repository.
func SealEventChainSegment(ctx context.Context, tx eventSegmentRunner, repositoryID string) (EventChainSegment, error) {
	headEventID, headHash, ok, err := eventChainHead(ctx, tx, repositoryID)
	if err != nil {
		return EventChainSegment{}, err
	}
	if !ok {
		return EventChainSegment{}, fmt.Errorf("%w: repository %q has no event chain head", ErrEventChainSegmentNoEvents, repositoryID)
	}
	return sealEventChainSegmentTo(ctx, tx, repositoryID, headEventID, headHash)
}

// sealEventChainSegmentTo is the core seal: it closes the open segment at the
// boundary (lastEventID/lastHash, already verified to be the chain head or an
// explicit anchored event) and opens the successor.
func sealEventChainSegmentTo(ctx context.Context, tx eventSegmentRunner, repositoryID string, lastEventID int64, lastHash string) (EventChainSegment, error) {
	open, err := ensureOpenEventSegment(ctx, tx, repositoryID)
	if err != nil {
		return EventChainSegment{}, err
	}

	// Empty-range guard: if the open segment already records a last boundary at or
	// beyond the requested boundary, there is nothing new to seal.
	if open.LastEventID != nil && *open.LastEventID >= lastEventID {
		return EventChainSegment{}, fmt.Errorf("%w: open segment %d already sealed through event %d (>= %d)",
			ErrEventChainSegmentNoEvents, open.SegmentID, *open.LastEventID, lastEventID)
	}

	// Determine the FIRST event of this segment. For the first segment it is the
	// earliest anchored event of the repository; for a successor it is the event
	// immediately after the predecessor's sealed boundary. We resolve it as the
	// lowest anchored event_id strictly greater than the predecessor's last
	// boundary (or the repository's minimum anchored event for the first segment).
	firstEventID, firstCreatedAt, firstHash, gotFirst, err := firstUnsealedEvent(ctx, tx, repositoryID, open.PreviousSegmentLastBoundary)
	if err != nil {
		return EventChainSegment{}, err
	}
	if !gotFirst {
		return EventChainSegment{}, fmt.Errorf("%w: repository %q has no anchored events past the prior seal", ErrEventChainSegmentNoEvents, repositoryID)
	}

	// Go-enforced referential integrity (D215): the boundary event must exist for
	// this repository AND carry the row hash we were handed. No SQL FK guards this.
	lastCreatedAt, ok, err := anchoredEventCreatedAt(ctx, tx, repositoryID, lastEventID, lastHash)
	if err != nil {
		return EventChainSegment{}, err
	}
	if !ok {
		return EventChainSegment{}, fmt.Errorf("%w: repository %q event %d (hash %q)",
			ErrEventChainSegmentBoundaryMissing, repositoryID, lastEventID, lastHash)
	}
	if lastEventID < firstEventID {
		return EventChainSegment{}, fmt.Errorf("%w: boundary event %d precedes segment first event %d",
			ErrEventChainSegmentBoundaryMissing, lastEventID, firstEventID)
	}

	// Seal the open segment in ONE write. The 'sealed' state freezes the row (the
	// refuse_sealed_event_segment_change trigger refuses any further UPDATE), so
	// every boundary column — including next_segment_first_previous_hash, the
	// witness the successor resumes from — must be set in this single sealing
	// UPDATE, never a second UPDATE on the now-frozen row.
	if err := tx.Exec(ctx, `
		UPDATE striatumd.event_chain_segments
		   SET state = 'sealed',
		       sealed_at = now(),
		       first_event_id = $3,
		       first_created_at = $4,
		       first_hash = $5,
		       last_event_id = $6,
		       last_created_at = $7,
		       last_hash = $8,
		       next_segment_first_previous_hash = $8
		 WHERE repository_id = $1 AND segment_id = $2`,
		repositoryID, open.SegmentID,
		firstEventID, firstCreatedAt, firstHash,
		lastEventID, lastCreatedAt, lastHash,
	); err != nil {
		return EventChainSegment{}, err
	}

	// Open the successor segment, chained to the segment just sealed. The
	// cross-segment witnesses are what let a verifier prove continuity across a
	// dropped-partition seam: previous_segment_last_hash records the seam hash,
	// and next_segment_first_previous_hash on the SEALED segment records that the
	// chain resumed at that same hash.
	successorID := open.SegmentID + 1
	if err := tx.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(
		  repository_id, segment_id, opened_at, state, retention_state,
		  previous_segment_id, previous_segment_last_hash
		) VALUES ($1, $2, now(), 'open', 'active', $3, $4)`,
		repositoryID, successorID, open.SegmentID, lastHash,
	); err != nil {
		return EventChainSegment{}, err
	}

	sealed := EventChainSegment{
		RepositoryID:                 repositoryID,
		SegmentID:                    open.SegmentID,
		State:                        "sealed",
		RetentionState:               "active",
		FirstEventID:                 &firstEventID,
		LastEventID:                  &lastEventID,
		FirstHash:                    &firstHash,
		LastHash:                     &lastHash,
		NextSegmentFirstPreviousHash: &lastHash,
	}
	if open.PreviousSegmentID != nil {
		sealed.PreviousSegmentID = open.PreviousSegmentID
	}
	if open.PreviousSegmentLastHash != nil {
		sealed.PreviousSegmentLastHash = open.PreviousSegmentLastHash
	}
	return sealed, nil
}

// openSegmentRow carries the open segment plus its inherited predecessor
// boundary (the event id the prior seal stopped at, used to scope the next
// segment's first event).
type openSegmentRow struct {
	SegmentID                   int64
	PreviousSegmentID           *int64
	PreviousSegmentLastHash     *string
	PreviousSegmentLastBoundary *int64
	LastEventID                 *int64
}

// ensureOpenEventSegment returns the repository's single open segment, creating
// segment 1 lazily if none exists. It locks the row FOR UPDATE so concurrent
// sealers serialize.
func ensureOpenEventSegment(ctx context.Context, tx eventSegmentRunner, repositoryID string) (openSegmentRow, error) {
	var row openSegmentRow
	var prevID, prevLastBoundary *int64
	var prevHash *string
	var lastEventID *int64
	err := tx.QueryRow(ctx, `
		SELECT s.segment_id, s.previous_segment_id, s.previous_segment_last_hash,
		       p.last_event_id, s.last_event_id
		  FROM striatumd.event_chain_segments s
		  LEFT JOIN striatumd.event_chain_segments p
		    ON p.repository_id = s.repository_id
		   AND p.segment_id = s.previous_segment_id
		 WHERE s.repository_id = $1 AND s.state = 'open'
		 FOR UPDATE OF s`,
		repositoryID,
	).Scan(&row.SegmentID, &prevID, &prevHash, &prevLastBoundary, &lastEventID)
	if err == nil {
		row.PreviousSegmentID = prevID
		row.PreviousSegmentLastHash = prevHash
		row.PreviousSegmentLastBoundary = prevLastBoundary
		row.LastEventID = lastEventID
		return row, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return openSegmentRow{}, err
	}
	// No open segment yet: open segment 1.
	if err := tx.Exec(ctx, `
		INSERT INTO striatumd.event_chain_segments(
		  repository_id, segment_id, opened_at, state, retention_state
		) VALUES ($1, 1, now(), 'open', 'active')`,
		repositoryID,
	); err != nil {
		return openSegmentRow{}, err
	}
	return openSegmentRow{SegmentID: 1}, nil
}

// eventChainHead returns the repository's current chain head (last anchored
// event id + hash) from repo_event_chain_heads, locking it FOR UPDATE so a
// concurrent append serializes against the seal.
func eventChainHead(ctx context.Context, tx eventSegmentRunner, repositoryID string) (int64, string, bool, error) {
	var eventID *int64
	var hash *string
	err := tx.QueryRow(ctx,
		"SELECT last_event_id, last_hash FROM striatumd.repo_event_chain_heads WHERE repository_id = $1 FOR UPDATE",
		repositoryID,
	).Scan(&eventID, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", false, nil
		}
		return 0, "", false, err
	}
	if eventID == nil || hash == nil {
		return 0, "", false, nil
	}
	return *eventID, *hash, true, nil
}

// firstUnsealedEvent finds the earliest anchored event of a repository strictly
// after the prior seal's boundary (or the minimum anchored event when there is
// none), returning its id, created_at, and row hash. This anchors the first
// boundary of the segment being sealed.
func firstUnsealedEvent(ctx context.Context, tx eventSegmentRunner, repositoryID string, afterEventID *int64) (int64, string, string, bool, error) {
	floor := int64(0)
	if afterEventID != nil {
		floor = *afterEventID
	}
	var eventID *int64
	var createdAt *string
	var hash *string
	err := tx.QueryRow(ctx, `
		SELECT event_id, created_at, row_hash
		  FROM striatumd.events
		 WHERE repository_id = $1
		   AND event_id > $2
		   AND row_hash IS NOT NULL
		 ORDER BY event_id ASC
		 LIMIT 1`,
		repositoryID, floor,
	).Scan(&eventID, &createdAt, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", "", false, nil
		}
		return 0, "", "", false, err
	}
	if eventID == nil || createdAt == nil || hash == nil {
		return 0, "", "", false, nil
	}
	return *eventID, *createdAt, *hash, true, nil
}

// anchoredEventCreatedAt is the Go referential-integrity check (D215): it
// confirms the boundary event exists for the repository AND its stored row_hash
// matches the hash the caller intends to seal, returning the event's created_at
// to record the time boundary. A mismatch or absence returns ok=false.
func anchoredEventCreatedAt(ctx context.Context, tx eventSegmentRunner, repositoryID string, eventID int64, expectHash string) (string, bool, error) {
	var createdAt *string
	var hash *string
	err := tx.QueryRow(ctx, `
		SELECT created_at, row_hash
		  FROM striatumd.events
		 WHERE repository_id = $1 AND event_id = $2`,
		repositoryID, eventID,
	).Scan(&createdAt, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}
	if createdAt == nil || hash == nil || *hash != expectHash {
		return "", false, nil
	}
	return *createdAt, true, nil
}
