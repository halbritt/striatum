package reads

import (
	"context"

	"github.com/halbritt/striatum/go/pkg/db"
)

// RFC 0136 P1 (D242) — the event_chain_segment_seam_unproven doctor invariant.
//
// P1 generalizes the audit_segments seal/boundary-hash model to the per-repo
// event chain (migration 0041 + pkg/mutations SealEventChainSegment). A SEALED
// segment is the unit of retention: a future partition DROP (P2/P4) may only
// retire a range whose segment is sealed and whose boundary hashes are durably
// recorded, so a verifier can prove continuity across the dropped-rows seam from
// the segment record alone.
//
// This check enforces the part of that invariant P1 already produces, with NO
// dependency on the (not-yet-built) retention executor:
//
//   1. SEAL COMPLETENESS — a sealed segment must carry both boundary hashes
//      (first_hash, last_hash) and both boundary event ids. A sealed segment with
//      a null boundary hash is a seal that cannot witness its own range — the
//      retention path must never drop a range it cannot prove.
//   2. SEAM CONTINUITY — a sealed segment that names a predecessor
//      (previous_segment_id) must carry previous_segment_last_hash equal to the
//      predecessor's last_hash, and the predecessor must record
//      next_segment_first_previous_hash equal to that same hash. A mismatch means
//      the cross-segment witness does not actually link the two sealed ranges, so
//      the chain's provable continuity is broken at the seam.
//
// Hard RED (a problem, not a warning): an unproven seam means a dropped partition
// would be indistinguishable from tampering (RFC 0136 §"Preserving the hash-chain
// contracts"). The check is repository-scoped and tolerates a DB on which
// migration 0041 has not yet applied (rollout window) by skipping when the table
// is absent.
func doctorEventChainSegmentSeams(ctx context.Context, runner db.Runner, repositoryID string) (map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"skipped": "repository_id required",
	}
	if repositoryID == "" {
		return block, nil, nil
	}

	exists, err := runner.QueryScalar(ctx,
		"SELECT (to_regclass('striatumd.event_chain_segments') IS NOT NULL)::text")
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		return block, []string{"event_chain_segment_seams.table_probe_failed: " + err.Error()}, []map[string]any{{
			"check": "event_chain_segment_seams_table_probe_failed",
			"error": err.Error(),
		}}
	}
	if exists != "true" {
		block["checked"] = true
		block["skipped"] = "striatumd.event_chain_segments not present; apply runtime migration 0041 to enable"
		return block, nil, nil
	}

	// One read covers both invariants: each sealed segment, left-joined to its
	// named predecessor, with the breach reason computed in SQL.
	rows, err := collectRows(ctx, runner, `
		SELECT s.segment_id,
		       s.previous_segment_id,
		       CASE
		         WHEN s.first_hash IS NULL OR s.last_hash IS NULL
		              OR s.first_event_id IS NULL OR s.last_event_id IS NULL
		           THEN 'seal_boundary_incomplete'
		         WHEN s.previous_segment_id IS NOT NULL AND p.segment_id IS NULL
		           THEN 'predecessor_missing'
		         WHEN s.previous_segment_id IS NOT NULL
		              AND s.previous_segment_last_hash IS DISTINCT FROM p.last_hash
		           THEN 'seam_hash_mismatch'
		         WHEN s.previous_segment_id IS NOT NULL
		              AND p.next_segment_first_previous_hash IS DISTINCT FROM s.previous_segment_last_hash
		           THEN 'predecessor_resume_witness_mismatch'
		         ELSE NULL
		       END AS breach
		  FROM striatumd.event_chain_segments s
		  LEFT JOIN striatumd.event_chain_segments p
		    ON p.repository_id = s.repository_id
		   AND p.segment_id = s.previous_segment_id
		 WHERE s.repository_id = $1
		   AND s.state IN ('sealed', 'purged')
		 ORDER BY s.segment_id`,
		repositoryID)
	if err != nil {
		block["checked"] = true
		block["skipped"] = nil
		block["error"] = err.Error()
		return block, []string{"event_chain_segment_seams.read_failed: " + err.Error()}, []map[string]any{{
			"check": "event_chain_segment_seams_read_failed",
			"error": err.Error(),
		}}
	}

	block["checked"] = true
	block["skipped"] = nil

	sealedCount := 0
	unproven := []map[string]any{}
	problems := []string{}
	records := []map[string]any{}
	for _, row := range rows {
		sealedCount++
		breach, _ := row["breach"].(string)
		if breach == "" {
			continue
		}
		segmentID := row["segment_id"]
		entry := map[string]any{
			"segment_id":          segmentID,
			"previous_segment_id": row["previous_segment_id"],
			"reason":              breach,
		}
		unproven = append(unproven, entry)
		problems = append(problems, "event_chain_segment_seam_unproven")
		records = append(records, map[string]any{
			"check":               "event_chain_segment_seam_unproven",
			"repository_id":       repositoryID,
			"segment_id":          segmentID,
			"previous_segment_id": row["previous_segment_id"],
			"reason":              breach,
		})
	}

	block["sealed_segments"] = sealedCount
	block["unproven_seams"] = unproven
	return block, problems, records
}
