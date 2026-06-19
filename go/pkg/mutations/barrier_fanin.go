package mutations

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// RFC 0135 P1 (D216) — the FIRST LIVE instance of the sealed expectation barrier:
// fan-in with entity=job, seal=attempt (#345). This is RFC 0133's Slice 2 barrier,
// lifted to consume the entity/seal-generic predicate P0 minted in
// db.BarrierReadySQL.
//
// CUTOVER DISCIPLINE (RFC 0135 / RFC 0133 "Migration and rollout"): this is an
// OPT-IN, shadow mechanism. The shipped D206 per-completion run-branch merge
// (fanInIntegrateRunBranch in worktree.go) stays the DEFAULT path. The barrier and
// the per-completion path produce the SAME final tree for any completion order —
// proven by TestFaninBarrierSameFinalTreeAsPerCompletion (the same-final-tree
// equivalence fixture) — before any workflow flips to the barrier. P2 (assembly +
// the barrier_assembly job type) and the default-flip come later; nothing here
// wires the barrier into the live completion path or changes the default.
//
// The trap-killer property (RFC 0133 synthesis trap #1, generalized to seal):
// readiness JOINs each declared in-edge's staged contribution against the seat's
// LIVE attempt (staged.attempt = jobs.attempt). A requeue/resume/complete-stalled
// bumps jobs.attempt to a strictly-new value, so a stale-attempt staging row is
// STRUCTURALLY invisible to the barrier — never counted, never filtered after the
// fact. There is no COUNT(*) of staged refs per seat.

const (
	// faninBarrierEntityKind is the entity discriminator for the fan-in instance.
	faninBarrierEntityKind = "job"
	// faninBarrierSealColumn is the seal column for the fan-in instance: the live
	// attempt. (For the revision-coherence caller, P5, the seal is
	// review_generation; the predicate is identical in shape.)
	faninBarrierSealColumn = "attempt"

	// stagedRefPrefix is the attempt-addressed staging-ref namespace
	// refs/striatum/staged/<run>/<job>/<attempt> (RFC 0133). The `staged/` verb
	// prefix keeps these OUT of the integrate sweep's refs/striatum/<run>/ glob, and
	// the attempt suffix makes a stale attempt's ref structurally distinct.
	stagedRefPrefix = "refs/striatum/staged/"
	// voidedRefPrefix is the requeue-tombstone namespace
	// refs/striatum/voided/<run>/<job>/<attempt> (RFC 0133): when a seat is
	// requeued, its prior staging ref is renamed here so recovery can EXPLAIN why a
	// ref was excluded, rather than the ref silently vanishing.
	voidedRefPrefix = "refs/striatum/voided/"
)

// stagingRef builds the attempt-addressed staging ref for a sibling completion.
func stagingRef(runID, workflowJobID string, attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("%s%s/%s/%d", stagedRefPrefix, runID, workflowJobID, attempt)
}

// voidedRef builds the requeue-tombstone ref for a superseded attempt.
func voidedRef(runID, workflowJobID string, attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("%s%s/%s/%d", voidedRefPrefix, runID, workflowJobID, attempt)
}

// faninBarrierReadinessSQL wraps the entity/seal-generic predicate P0 minted in
// db.BarrierReadySQL (entity=job, seal=attempt) with the three relations it JOINs,
// derived in-query from the real freeze record + staging table + striatumd.jobs.
// It is built ONCE here so the fan-in caller cannot drift from the canonical shape;
// the static guard TestBarrierPredicateHasNoRefCount scans this file and fails the
// build on any bare ref-COUNT(*) / recency-latest barrier shape.
//
// The three relations the predicate expects (all keyed on (entity_kind, entity_id)):
//
//   - barrier_in_edges  edge   — one row per DECLARED sibling seat, carrying
//     is_terminal_gap (TRUE when the seat is quarantined — RFC 0133
//     quarantine-as-terminal-in-edge, so a quarantined sibling does not deadlock
//     the barrier forever; OR a sealed gap admitted in Go — RFC 0138, see below).
//     entity_id is the stable workflow_job_id.
//   - entity_live_seal  live   — each seat's LIVE attempt (MAX(attempt) over the
//     COMPLETED job rows for that workflow_job_id). This is the load-bearing
//     relation: the entity's live seal, NOT a per-edge count.
//   - staged_contrib    staged — each seat's live staging row (the highest-attempt
//     non-voided staged contribution). A `recovery/`-prefixed staging ref is
//     EXCLUDED so a complete-stalled recovery ref cannot silently enter the
//     canonical set (RFC 0133). A `voided` (tombstoned) row is excluded too.
//
// RFC 0138 (#453) — the SEALED-GAP disjunct. `sealedGapSeats` is the set of
// workflow_job_ids the caller has resolved (in Go, faninBarrierReady →
// resolveFaninSealedGapSeats) as admissible terminal gaps: a barrier that explicitly
// OPTED IN (fanin_tolerates_sealed_gap) over a PROVABLY-DEAD required seat
// (supervisedAgentConfirmedDead — the SAME oracle the quorum path reuses), within the
// max_sealed_gaps budget. Those seats are OR'd into is_terminal_gap exactly the way a
// quarantined seat already is — composing with the RFC 0135 predicate's
// is_terminal_gap disjunct, NOT forking it. With an empty set (the default, opt-in
// off), the SQL is byte-for-byte the prior behavior. The liveness probe is a Go
// oracle, never a SQL predicate (a slow seat must never be a gap), so the SQL admits
// the gap only over the seats the Go resolver already vetted as provably dead.
//
// The barrier id binds as $1 and the entity kind as $2 (the predicate's contract);
// $3 binds the repository id, scoping every CTE.
func faninBarrierReadinessSQL(sealedGapSeats []string) (string, error) {
	predicate, err := db.BarrierReadySQL(db.BarrierSpec{
		EntityKind:         faninBarrierEntityKind,
		InEdgesTable:       "barrier_in_edges",
		LiveSealTable:      "entity_live_seal",
		StagedContribTable: "staged_contrib",
		SealColumn:         faninBarrierSealColumn,
	})
	if err != nil {
		return "", err
	}
	// The CTEs supply the predicate's three relations. The freeze record's
	// declared_sibling_job_ids array expands into one in-edge per seat; the live
	// seal is MAX(attempt) over the seat's COMPLETED jobs; the staged contribution
	// is the seat's highest-attempt non-voided, non-recovery-prefixed staging row.
	// Note: a quarantined seat's is_terminal_gap is sourced from striatumd.jobs
	// state = 'canceled' WITH a recorded quarantine (RFC 0133 / #311 P0); a seat
	// with no completed job and no quarantine is neither live-sealed (live row
	// absent) nor terminal, so the INNER JOIN on entity_live_seal drops it and the
	// bool_and over the remaining edges cannot be TRUE while it is outstanding —
	// UNLESS the seat is an admitted sealed gap (RFC 0138), which the
	// sealed_gap_seats CTE marks is_terminal_gap so it fires WITH a recorded gap.
	return `WITH frozen AS (
    SELECT barrier_id, run_id, declared_sibling_job_ids
      FROM striatumd.fanin_freeze_points
     WHERE repository_id = $3 AND barrier_id = $1
), declared AS (
    SELECT '` + faninBarrierEntityKind + `'::text AS entity_kind,
           sib.value::text AS entity_id,
           frozen.run_id  AS run_id
      FROM frozen,
           jsonb_array_elements_text(frozen.declared_sibling_job_ids) AS sib(value)
), sealed_gap_seats AS (
    -- RFC 0138: the seats the Go resolver admitted as sealed terminal gaps
    -- (opted-in barrier + provably-dead seat + within budget). Empty by default.
    ` + sealedGapSeatsCTEBody(sealedGapSeats) + `
), barrier_in_edges AS (
    SELECT d.entity_kind,
           d.entity_id,
           $1::text AS barrier_id,
           -- A quarantined seat (canceled with a quarantine marker) is a
           -- terminal-acceptable gap: the barrier fires WITH a recorded gap in the
           -- manifest rather than deadlocking. Resolved against the live job rows.
           -- RFC 0138: a Go-admitted sealed gap (sg.entity_id present) is ALSO a
           -- terminal gap — the same is_terminal_gap disjunct (RFC 0135), so the
           -- predicate is unchanged.
           (COALESCE(bool_or(j.state = 'canceled' AND COALESCE(j.write_scope_json->>'quarantined','') = 'true'), false)
            OR bool_or(sg.entity_id IS NOT NULL)) AS is_terminal_gap
      FROM declared d
      LEFT JOIN striatumd.jobs j
        ON j.repository_id = $3 AND j.run_id = d.run_id AND j.workflow_job_id = d.entity_id
      LEFT JOIN sealed_gap_seats sg ON sg.entity_id = d.entity_id
     GROUP BY d.entity_kind, d.entity_id
), live_completed AS (
    -- The live seal (MAX completed attempt) per seat, only for seats with a
    -- completed job. A seat with no completed job is absent here.
    SELECT j.workflow_job_id AS entity_id,
           MAX(j.attempt) AS attempt
      FROM striatumd.jobs j
      JOIN frozen ON j.repository_id = $3 AND j.run_id = frozen.run_id
     WHERE j.state = 'completed'
       AND j.workflow_job_id IN (SELECT entity_id FROM declared)
     GROUP BY j.workflow_job_id
), entity_live_seal AS (
    -- One row PER DECLARED in-edge so the predicate's INNER JOIN never DROPS an
    -- uncompleted seat (which would make bool_and ignore it and fire prematurely).
    -- An uncompleted seat gets live seal 0; attempts are >=1 and the staged
    -- sentinel is -1, so live(0) never equals staged(-1) — the edge is FALSE, and a
    -- live but unstaged completed seat (live=N, staged=-1) is FALSE too. The barrier
    -- fires only when every seat is staged at its live completed attempt.
    SELECT d.entity_kind,
           d.entity_id,
           COALESCE(lc.attempt, 0) AS attempt
      FROM declared d
      LEFT JOIN live_completed lc ON lc.entity_id = d.entity_id
), live_staged AS (
    -- The highest-attempt non-voided, non-recovery staging row per seat.
    SELECT s.workflow_job_id AS entity_id,
           MAX(s.attempt) AS attempt
      FROM striatumd.barrier_staged_contributions s
     WHERE s.repository_id = $3 AND s.barrier_id = $1
       AND s.status = 'staged'
       -- A complete-stalled recovery ref cannot silently enter the canonical set.
       AND s.staging_ref NOT LIKE 'refs/striatum/recovery/%'
     GROUP BY s.workflow_job_id
), staged_contrib AS (
    -- One row PER DECLARED in-edge so the predicate's LEFT JOIN always matches and
    -- an UNSTAGED seat yields a non-NULL, never-equal sentinel seal (-1) — the
    -- disjunct staged.attempt = live.attempt then evaluates to FALSE, not NULL, so
    -- bool_and FAILS CLOSED on an outstanding seat (a NULL would be silently
    -- skipped by bool_and and let an incomplete barrier fire).
    SELECT d.entity_kind,
           d.entity_id,
           COALESCE(ls.attempt, -1) AS attempt
      FROM declared d
      LEFT JOIN live_staged ls ON ls.entity_id = d.entity_id
)
` + predicate, nil
}

// faninBarrierReady evaluates the live-seal JOIN barrier for one fan-in barrier
// under the per-run advisory lock the caller already holds (RFC 0104 — the
// advisory-lock fire serialization). It returns whether the barrier is ready to
// fire (every declared in-edge is live-staged or a terminal gap), the per-edge
// classification used to build the manifest, and the freeze record. A NULL
// bool_and (no declared in-edges resolvable yet) is treated as NOT ready.
//
// RFC 0138 (#453): BEFORE evaluating the SQL it resolves the admissible SEALED GAPS
// in Go (resolveFaninSealedGapSeats — opted-in barrier + provably-dead seats + budget)
// and threads them into the predicate's is_terminal_gap disjunct. With the opt-in off
// (the default) this resolves to the empty set, so the readiness is byte-for-byte the
// prior behavior. The liveness probe is a Go oracle (a merely-slow seat is NEVER a
// gap), so a sealed gap is admitted only over seats the resolver vetted as provably
// dead.
func faninBarrierReady(ctx context.Context, runner db.TxRunner, repositoryID, barrierID string) (bool, error) {
	sealedGaps, err := resolveFaninSealedGapSeats(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return false, err
	}
	query, err := faninBarrierReadinessSQL(sealedGaps)
	if err != nil {
		return false, err
	}
	row := runner.QueryRow(ctx, query, barrierID, faninBarrierEntityKind, repositoryID)
	var ready *bool
	if err := row.Scan(&ready); err != nil {
		return false, fmt.Errorf("fan-in barrier readiness query: %w", err)
	}
	return ready != nil && *ready, nil
}

// sealedGapSeatsCTEBody renders the body of the sealed_gap_seats CTE: a VALUES list
// of the admitted gap seats' entity_ids, or an empty-set SELECT when there are none.
// The seat ids are workflow_job_ids (daemon-generated identifiers); they are quoted
// as SQL string literals with single-quote doubling for defense-in-depth, exactly as
// the quorum caller quotes its VALUES-built seat ids (barrier_quorum.go). They are
// data rows, never interpolated into the predicate's identifier positions.
func sealedGapSeatsCTEBody(seats []string) string {
	if len(seats) == 0 {
		// An empty set: a typed, zero-row relation the LEFT JOIN matches nothing in.
		return "SELECT NULL::text AS entity_id WHERE false"
	}
	values := make([]string, 0, len(seats))
	for _, s := range seats {
		values = append(values, "('"+strings.ReplaceAll(s, "'", "''")+"')")
	}
	return "SELECT entity_id FROM (VALUES " + strings.Join(values, ", ") + ") AS sg(entity_id)"
}

// faninSealedGapDamageCode is the join_manifest.v1 damage_code recorded for a seat the
// barrier fired WITHOUT because it was a provably-dead required seat the author opted
// to tolerate (RFC 0138). It is the fan-in analog of the quarantine seat's
// `seat_quarantined` code — a loud, durable marker that the join is SHORT a required
// leg, so a downstream gate can refuse a degraded manifest.
const faninSealedGapDamageCode = "required_seat_unrecoverable"

// resolveFaninSealedGapSeats returns the workflow_job_ids of the declared sibling
// seats this barrier may fire WITHOUT as sealed terminal gaps (RFC 0138, Option B).
// It is the fan-in analog of resolvePanelSeats' abstention-budget admission, reusing
// the SAME forgery-resistant supervisedAgentConfirmedDead oracle via
// seatStructurallyUnrecoverable.
//
// A seat is admitted ONLY when ALL of:
//
//   - the barrier explicitly OPTED IN (fanin_tolerates_sealed_gap on the freeze
//     record). With the opt-in off — the default — this returns nil immediately, so
//     every existing strict fan-in is unchanged (Option A behavior).
//   - the seat is OUTSTANDING: it has no live-seal staged contribution and is not
//     already a quarantine terminal gap (a satisfied or quarantined seat needs no
//     sealed gap).
//   - the seat is PROVABLY DEAD: its live-attempt job's owning lane is dead per
//     supervisedAgentConfirmedDead (a merely-slow seat is NEVER a gap — silence from
//     a live lane is not consent, the D214b axiom carried over from the quorum path).
//   - it is WITHIN the max_sealed_gaps budget: of the provably-dead outstanding seats,
//     at most max_sealed_gaps are admitted (deterministically, sorted by seat id); a
//     dead seat BEYOND the budget stays blocking, mirroring resolvePanelSeats. So a
//     join is never shorter than the author declared tolerable.
//
// It NEVER seals a seat the author did not opt in for, a seat that is merely slow, or
// a seat beyond the budget — so completeness is never silently forged.
func resolveFaninSealedGapSeats(ctx context.Context, runner db.TxRunner, repositoryID, barrierID string) ([]string, error) {
	fp, err := loadFaninFreezePoint(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return nil, err
	}
	// Opt-in off (default) or a zero budget: no sealed gap is ever admitted (Option A).
	if !fp.ToleratesSealedGap || fp.MaxSealedGaps <= 0 {
		return nil, nil
	}
	// Resolve which declared seats are OUTSTANDING (not live-staged, not quarantined),
	// with each seat's live attempt — exactly the seats that could need a sealed gap.
	rows, err := queryRows(ctx, runner, `
		WITH declared AS (
		    SELECT sib.value::text AS workflow_job_id
		      FROM striatumd.fanin_freeze_points fp,
		           jsonb_array_elements_text(fp.declared_sibling_job_ids) AS sib(value)
		     WHERE fp.repository_id = $1 AND fp.barrier_id = $2
		)
		SELECT d.workflow_job_id,
		       COALESCE((
		         SELECT MAX(j.attempt) FROM striatumd.jobs j
		          WHERE j.repository_id = $1 AND j.run_id = $3
		            AND j.workflow_job_id = d.workflow_job_id
		       ), 0) AS live_attempt,
		       COALESCE((
		         SELECT bool_or(j.state = 'canceled'
		                        AND COALESCE(j.write_scope_json->>'quarantined','') = 'true')
		           FROM striatumd.jobs j
		          WHERE j.repository_id = $1 AND j.run_id = $3
		            AND j.workflow_job_id = d.workflow_job_id
		       ), false) AS is_quarantined,
		       EXISTS (
		         SELECT 1 FROM striatumd.barrier_staged_contributions s
		          WHERE s.repository_id = $1 AND s.barrier_id = $2
		            AND s.workflow_job_id = d.workflow_job_id
		            AND s.status = 'staged'
		            AND s.staging_ref NOT LIKE 'refs/striatum/recovery/%'
		            AND s.attempt = COALESCE((
		                  SELECT MAX(j.attempt) FROM striatumd.jobs j
		                   WHERE j.repository_id = $1 AND j.run_id = $3
		                     AND j.workflow_job_id = d.workflow_job_id
		                     AND j.state = 'completed'
		                ), -1)
		       ) AS live_staged
		  FROM declared d
		 ORDER BY d.workflow_job_id`,
		repositoryID, barrierID, fp.RunID)
	if err != nil {
		return nil, err
	}
	// Of the OUTSTANDING seats, find the PROVABLY-DEAD ones (the same oracle the
	// quorum reuses). A seat with no live-attempt job, or whose pointer is not
	// probeable, is NOT provably dead → it stays blocking.
	deadCandidates := make([]string, 0, len(rows))
	for _, r := range rows {
		seat := fmt.Sprint(r["workflow_job_id"])
		if boolValue(r["live_staged"]) || boolValue(r["is_quarantined"]) {
			continue // satisfied or already a terminal gap — no sealed gap needed
		}
		liveAttempt := intValue(r["live_attempt"])
		if liveAttempt <= 0 {
			// No live-attempt job row to probe → not provably dead → blocks.
			continue
		}
		dead, derr := seatStructurallyUnrecoverable(ctx, runner, repositoryID, fp.RunID, seat, liveAttempt)
		if derr != nil {
			return nil, derr
		}
		if dead {
			deadCandidates = append(deadCandidates, seat)
		}
	}
	// Apply the budget: admit at most MaxSealedGaps provably-dead seats as sealed gaps
	// (deterministically — deadCandidates is already sorted by seat id). A dead seat
	// beyond the budget is NOT admitted and stays blocking (the join may not be shorter
	// than the author declared tolerable).
	if len(deadCandidates) > fp.MaxSealedGaps {
		deadCandidates = deadCandidates[:fp.MaxSealedGaps]
	}
	if len(deadCandidates) == 0 {
		return nil, nil
	}
	return deadCandidates, nil
}

// faninFreezePoint is the immutable freeze record written once at fan-out.
type faninFreezePoint struct {
	BarrierID               string
	RunID                   string
	DownstreamWorkflowJobID string
	FrozenTipSHA            string
	FrozenTipTreeSHA        string
	DeclaredSiblingJobIDs   []string
	// ToleratesSealedGap is the RFC 0138 (#453) per-barrier opt-in: when true, the
	// barrier may fire DEGRADED over up to MaxSealedGaps provably-dead REQUIRED seats
	// (a sealed terminal gap), instead of parking the run in needs_operator. Default
	// false — a dead required seat is an operator decision (Option A). Sealed with the
	// barrier at fan-out (the freeze record is the immutable opt-in declaration).
	ToleratesSealedGap bool
	// MaxSealedGaps is the budget: how many provably-dead required legs the join may be
	// short (the fan-in analog of the quorum's max_gating_abstentions). Default 0 — even
	// with ToleratesSealedGap true, a 0 budget admits no gap.
	MaxSealedGaps int
}

// recordFaninFreezePoint writes the immutable freeze record. It is append-only (the
// migration's SELECT/INSERT-only grant + refuse-trigger make a later UPDATE/DELETE
// impossible), so a re-fire that observes an existing freeze point treats it as the
// canonical base rather than re-pointing it. The frozen tip is the run-branch tip
// at fan-out; the assembly folds staged refs onto it.
func recordFaninFreezePoint(ctx context.Context, runner db.TxRunner, repositoryID string, fp faninFreezePoint) error {
	declared, err := json.Marshal(fp.DeclaredSiblingJobIDs)
	if err != nil {
		return fmt.Errorf("marshal declared sibling job ids: %w", err)
	}
	var tree any
	if strings.TrimSpace(fp.FrozenTipTreeSHA) != "" {
		tree = fp.FrozenTipTreeSHA
	}
	// RFC 0138: write the sealed-gap tolerance columns when migration 0039 has landed.
	// A deployment BEHIND on 0039 lacks the columns, so detect them and fall back to
	// the prior INSERT (no opt-in possible — Option A behavior), exactly the degrade-
	// safe pattern (a barrier authored before 0039 simply cannot tolerate a gap).
	if faninSealedGapColumnsPresent(ctx, runner) {
		return runner.Exec(ctx, `
			INSERT INTO striatumd.fanin_freeze_points
			  (repository_id, barrier_id, run_id, downstream_workflow_job_id,
			   frozen_tip_sha, frozen_tip_tree_sha, declared_sibling_job_ids,
			   fanin_tolerates_sealed_gap, max_sealed_gaps)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)
			ON CONFLICT (repository_id, barrier_id) DO NOTHING`,
			repositoryID, fp.BarrierID, fp.RunID, fp.DownstreamWorkflowJobID,
			fp.FrozenTipSHA, tree, string(declared),
			fp.ToleratesSealedGap, fp.MaxSealedGaps)
	}
	return runner.Exec(ctx, `
		INSERT INTO striatumd.fanin_freeze_points
		  (repository_id, barrier_id, run_id, downstream_workflow_job_id,
		   frozen_tip_sha, frozen_tip_tree_sha, declared_sibling_job_ids)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (repository_id, barrier_id) DO NOTHING`,
		repositoryID, fp.BarrierID, fp.RunID, fp.DownstreamWorkflowJobID,
		fp.FrozenTipSHA, tree, string(declared))
}

// faninSealedGapColumnsPresent reports whether striatumd.fanin_freeze_points carries
// the RFC 0138 (#453) sealed-gap tolerance columns (migration 0039 applied). Mirrors
// dissentLedgerEnabled: on a deployment BEHIND on 0039 the columns are absent and the
// barrier degrades to Option A (no sealed gap admitted, a dead required seat parks the
// run) rather than erroring. The probe is cheap and the result stable per-deployment.
func faninSealedGapColumnsPresent(ctx context.Context, runner any) bool {
	row, err := oneRow(ctx, runner, `
		SELECT EXISTS (
		  SELECT 1 FROM information_schema.columns
		   WHERE table_schema = 'striatumd'
		     AND table_name = 'fanin_freeze_points'
		     AND column_name = 'fanin_tolerates_sealed_gap'
		) AS present`)
	return err == nil && boolValue(row["present"])
}

// stageFaninContribution records a sibling's completion as an attempt-addressed
// staging contribution AND cuts the staging git ref, co-transactionally with the
// caller's attempt/state update (RFC 0133). Provenance gates run before the row is
// written, so a contaminated contribution can never enter the canonical set, while
// a LEGITIMATE base drift is recovered rather than refused.
//
// The staged commit is classified against the frozen tip (classifyContributionBase):
//
//  1. DIRECT descendant — the staged commit descends from the frozen tip (the
//     common case). The #352 per-commit tree-provenance check then asserts every
//     commit in frozen_tip..staged was authored LINEARLY on top of the frozen tip,
//     closing the smuggled-content hole (a merge grafting an off-base side branch
//     whose content does not itself descend from the frozen tip).
//  2. RECOVERABLE base drift (#353, RFC 0133 Risks "Base drift (#299/#306)") — the
//     run branch evolved under the sibling's feet (e.g. another sibling integrated),
//     so the sibling authored its change on the EVOLVED tip, not the frozen tip. The
//     staged commit does NOT descend from the frozen tip but SHARES a real merge-base
//     with it (same lineage tree, no foreign root) and 3-way-merges onto the frozen
//     tip cleanly. We RECORD the recoverable rebase leg (the evolved base as the
//     drift-onto sha) so the assembly folds the contribution against the frozen tip
//     with an extra commit-tree parent — "a recorded, recoverable extra commit-tree
//     parent leg, not a CAS wedge." The drift-adapted tree-provenance check (anchored
//     at the merge-base) keeps the smuggled-content guarantee for this path too.
//  3. CONTAMINATED base — the staged chain folds in an off-base FOREIGN root lineage
//     the sibling never authored on top of any shared base (no merge-base, or a root
//     reachable from the staged commit that the frozen tip does not share). Still
//     REFUSED (barrier_smuggled_content, #352).
//
// The PG row is the authoritative record the barrier JOINs against the live
// attempt; the git ref is the witness. The row is keyed on
// (repository_id, barrier_id, workflow_job_id, attempt) so a re-stage of the same
// attempt is idempotent and a new attempt is a distinct row.
func stageFaninContribution(ctx context.Context, runner db.TxRunner, repoRoot, repositoryID, barrierID, runID, workflowJobID, jobID, commitSHA string, attempt int) (string, error) {
	fp, err := loadFaninFreezePoint(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return "", err
	}
	class, driftBase, err := classifyContributionBase(ctx, repoRoot, fp, workflowJobID, commitSHA, attempt)
	if err != nil {
		return "", err
	}
	// Tree-provenance check (#352): close the smuggled-content hole the ancestry
	// check cannot see (RFC 0133 Risks). For a direct descendant we measure provenance
	// against the frozen tip; for a recoverable drift we measure it against the shared
	// merge-base (the evolved-base lineage is legitimate, so the merge-base is the
	// trusted anchor the contribution must descend from with no foreign graft).
	provenanceBase := fp.FrozenTipSHA
	sealTreeAnchor := true
	if class == contributionBaseDrift {
		provenanceBase = driftBase
		sealTreeAnchor = false // the merge-base is not the frozen tip; its tree is not the sealed one
	}
	if err := assertContributionTreeProvenance(ctx, repoRoot, fp, provenanceBase, sealTreeAnchor, workflowJobID, commitSHA, attempt); err != nil {
		return "", err
	}
	ref := stagingRef(runID, workflowJobID, attempt)
	if _, exit, err := integrateGit(ctx, repoRoot, "update-ref", ref, commitSHA); err != nil {
		return "", err
	} else if exit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("could not write fan-in staging ref %q for seat %s", ref, workflowJobID), nil)
	}
	// Record the recoverable drift leg (NULL for a direct descendant). base_drift_onto_sha
	// is the evolved base the assembly folds the contribution against as an extra
	// commit-tree parent; base_drift_reason makes the recovery legible.
	var driftOnto, driftReason any
	if class == contributionBaseDrift {
		driftOnto = driftBase
		driftReason = "evolved_run_branch"
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.barrier_staged_contributions
		  (repository_id, barrier_id, run_id, workflow_job_id, attempt, job_id, staging_ref, commit_sha, status,
		   base_drift_onto_sha, base_drift_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'staged', $9, $10)
		ON CONFLICT (repository_id, barrier_id, workflow_job_id, attempt)
		DO UPDATE SET job_id = EXCLUDED.job_id,
		              staging_ref = EXCLUDED.staging_ref,
		              commit_sha = EXCLUDED.commit_sha,
		              status = 'staged',
		              voided_at = NULL,
		              void_reason = NULL,
		              base_drift_onto_sha = EXCLUDED.base_drift_onto_sha,
		              base_drift_reason = EXCLUDED.base_drift_reason`,
		repositoryID, barrierID, runID, workflowJobID, attempt, jobID, ref, commitSHA, driftOnto, driftReason); err != nil {
		return "", err
	}
	return ref, nil
}

// contributionBaseClass classifies a staged fan-in contribution against the frozen
// tip (RFC 0133 #353 base-drift-as-a-recoverable-leg).
type contributionBaseClass int

const (
	// contributionBaseDirect: the staged commit descends from the frozen tip.
	contributionBaseDirect contributionBaseClass = iota
	// contributionBaseDrift: the staged commit does NOT descend from the frozen tip
	// but shares a real merge-base with it and folds in no foreign root — a
	// legitimate evolved-run-branch drift that is RECOVERABLE.
	contributionBaseDrift
)

// classifyContributionBase decides whether a staged contribution is a direct
// descendant of the frozen tip, a recoverable base drift, or a contaminated base
// (refused). For a recoverable drift it returns the merge-base of the frozen tip and
// the staged commit — the evolved base the assembly folds the contribution against
// as an extra commit-tree parent leg (RFC 0133 Risks "Base drift (#299/#306)").
//
// The discriminator between RECOVERABLE drift and CONTAMINATED base:
//
//   - A merge-base of (frozen, staged) MUST exist — disjoint histories (no shared
//     ancestor) mean the contribution shares no lineage with the frozen tip at all,
//     the strongest contamination signal. Refused.
//   - EVERY root commit reachable from the staged commit MUST also be reachable from
//     the frozen tip. A root reachable from staged but NOT from frozen is a FOREIGN
//     lineage smuggled in via a merge (exactly the #352 off-base-root shape) — even
//     though a merge-base may exist on the other parent. Refused.
//
// When both hold the drift is recoverable: the staged commit forked off an evolved
// run-branch tip whose lineage descends from the same root(s) as the frozen tip, so
// the merge-base is a real common ancestor and the contribution 3-way-merges onto
// the frozen tip cleanly. (A residual graft inside the merge-base..staged range is
// still caught by the drift-adapted tree-provenance check the caller runs next.)
func classifyContributionBase(ctx context.Context, repoRoot string, fp faninFreezePoint, workflowJobID, commitSHA string, attempt int) (contributionBaseClass, string, error) {
	frozen := fp.FrozenTipSHA
	descends, err := gitIsAncestor(ctx, repoRoot, frozen, commitSHA)
	if err != nil {
		return 0, "", err
	}
	if descends {
		return contributionBaseDirect, "", nil
	}
	refuseContaminated := func(reason string) (contributionBaseClass, string, error) {
		return 0, "", rpc.NewError("barrier_smuggled_content", fmt.Sprintf(
			"fan-in staging refused for seat %s (attempt %d): staged commit %s does not descend from the frozen base %s and is not a recoverable base drift — %s. A fan-in contribution must descend from the frozen base or evolve from its lineage; an off-base foreign lineage is not a legitimate contribution (RFC 0133 Risks / #353 base-drift, #352 smuggled-content).",
			workflowJobID, attempt, commitSHA, frozen, reason),
			map[string]any{"workflow_job_id": workflowJobID, "attempt": attempt, "frozen_tip": frozen, "staged_commit": commitSHA})
	}
	// A merge-base must exist — disjoint histories are contamination.
	mergeBase, err := gitMergeBase(ctx, repoRoot, frozen, commitSHA)
	if err != nil {
		return 0, "", err
	}
	if mergeBase == "" {
		c, s, e := refuseContaminated("the staged commit shares no common ancestor with the frozen base (disjoint history)")
		return c, s, e
	}
	// Every root reachable from staged must also be reachable from frozen — a foreign
	// root in staged's history is an off-base lineage smuggled in via a merge.
	stagedRoots, err := gitRootCommits(ctx, repoRoot, commitSHA)
	if err != nil {
		return 0, "", err
	}
	frozenRoots, err := gitRootCommits(ctx, repoRoot, frozen)
	if err != nil {
		return 0, "", err
	}
	frozenRootSet := map[string]bool{}
	for _, r := range frozenRoots {
		frozenRootSet[strings.ToLower(r)] = true
	}
	for _, r := range stagedRoots {
		if !frozenRootSet[strings.ToLower(r)] {
			c, s, e := refuseContaminated(fmt.Sprintf("the staged commit's history includes an off-base root %s the frozen base does not share (foreign lineage smuggled via a merge)", r))
			return c, s, e
		}
	}
	return contributionBaseDrift, mergeBase, nil
}

// assertContributionTreeProvenance closes the smuggled-content hole RFC 0133's
// Risks section calls out (#352): the merge-base ancestry check proves the staged
// commit DESCENDS from the base, but a staged ref of `base + linear commits` passes
// it even when an intermediate MERGE commit grafted in foreign content via a side
// parent the sibling lane never authored off the base. Ancestry sees the topology;
// it cannot see that a foreign tree was folded in.
//
// `base` is the trusted lineage anchor the contribution must be authored forward
// from. For a DIRECT-descendant contribution it is the frozen tip (and
// sealTreeAnchor seals the stored frozen_tip_tree_sha against the live tree). For a
// RECOVERABLE base drift (#353) it is the merge-base of the frozen tip and the
// staged commit — the legitimate evolved-base lineage — so the same per-commit
// guarantee holds for the drift path: no foreign graft may enter the base..staged
// range even when the contribution evolved off a moved run-branch tip.
//
// THE ASSERTION (per-commit tree provenance): every commit in the contribution chain
// base..staged (the commits reachable from staged but not from the base — exactly
// what the sibling authored on top of the base) must itself DESCEND from the base.
// We prove this by connectivity rather than N git calls: a chain commit descends from
// the base iff it reaches the base by a parent walk that stays inside the chain (the
// range already excludes everything reachable from base, so the only legitimate
// boundary parent is the base itself). Seed the descendant set with the base's direct
// children and propagate forward over child edges; any chain commit NOT reached is
// grafted from outside the base lineage — an off-base merge parent or an independent
// root commit pulled in by a merge — and the contribution is REFUSED. So every tree
// mutation folded into the join was authored forward from the base's tree, never
// imported from an outside lineage.
//
// The stored frozen_tip_tree_sha is the trusted tree anchor for the DIRECT path:
// before walking we verify it equals the actual tree of frozen_tip_sha (when the
// freeze record carries one — older records may not), so the base cannot have been
// re-pointed under us. (The freeze record is structurally immutable, so this is a
// defense-in-depth seal, not the primary guarantee.) For the drift path the anchor
// is the merge-base, whose tree is not the sealed frozen tree, so sealTreeAnchor is
// false there.
func assertContributionTreeProvenance(ctx context.Context, repoRoot string, fp faninFreezePoint, base string, sealTreeAnchor bool, workflowJobID, commitSHA string, attempt int) error {
	frozen := fp.FrozenTipSHA
	refuse := func(reason string, smuggled string) error {
		return rpc.NewError("barrier_smuggled_content", fmt.Sprintf(
			"fan-in staging refused for seat %s (attempt %d): staged commit %s smuggles content into the join — %s. A fan-in contribution must be authored linearly on top of its base %s; a commit pulling in an off-base side branch is not a legitimate contribution (RFC 0133 Risks / #352).",
			workflowJobID, attempt, commitSHA, reason, base),
			map[string]any{
				"workflow_job_id": workflowJobID,
				"attempt":         attempt,
				"frozen_tip":      frozen,
				"provenance_base": base,
				"staged_commit":   commitSHA,
				"smuggled_commit": smuggled,
				"frozen_tip_tree": fp.FrozenTipTreeSHA,
			})
	}

	// Seal the tree anchor (DIRECT path only): the stored frozen_tip_tree_sha must
	// equal the live tree of the frozen tip, so provenance is measured against the
	// recorded base tree. (For a drift the base is the merge-base, not the frozen tip.)
	if sealTreeAnchor && strings.TrimSpace(fp.FrozenTipTreeSHA) != "" {
		actualTree, err := gitTreeOf(ctx, repoRoot, frozen)
		if err != nil {
			return err
		}
		if !strings.EqualFold(actualTree, strings.TrimSpace(fp.FrozenTipTreeSHA)) {
			return rpc.NewError("barrier_smuggled_content", fmt.Sprintf(
				"fan-in staging refused for seat %s (attempt %d): the frozen tip %s now resolves to tree %s but the immutable freeze record sealed tree %s — the base the join measures provenance against was re-pointed (RFC 0133 / #352).",
				workflowJobID, attempt, frozen, actualTree, fp.FrozenTipTreeSHA),
				map[string]any{"workflow_job_id": workflowJobID, "attempt": attempt, "frozen_tip": frozen, "frozen_tip_tree": fp.FrozenTipTreeSHA, "actual_tree": actualTree})
		}
	}

	// Enumerate the contribution chain base..staged with each commit's parents.
	// `--parents` prints "<commit> <parent1> [<parent2> ...]" per line; the range
	// base..staged excludes the base and everything reachable from it, so the chain is
	// exactly the commits the sibling authored on top of the base.
	out, exit, err := integrateGit(ctx, repoRoot, "rev-list", "--parents", base+".."+commitSHA)
	if err != nil {
		return err
	}
	if exit != 0 {
		return rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
			"fan-in tree-provenance walk failed for seat %s: could not enumerate %s..%s", workflowJobID, base, commitSHA), nil)
	}

	// Parse the chain into commit->parents and build the reverse (parent->children)
	// adjacency so we can propagate "descends from base" forward over child edges.
	baseLower := strings.ToLower(base)
	chain := map[string][]string{} // commit -> parents (all lowercased), chain commits only
	children := map[string][]string{}
	for _, raw := range strings.Split(out, "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		commit := strings.ToLower(fields[0])
		if !isFullGitSHA(commit) {
			continue
		}
		parents := make([]string, 0, len(fields)-1)
		for _, p := range fields[1:] {
			lp := strings.ToLower(p)
			parents = append(parents, lp)
			children[lp] = append(children[lp], commit)
		}
		chain[commit] = parents
	}

	// Seed the descendant frontier with the base's DIRECT children inside the chain
	// (a chain commit one of whose parents is the base), then BFS forward over child
	// edges. Every commit reached this way provably descends from the base through a
	// path that stays inside the chain.
	descends := map[string]bool{}
	frontier := append([]string(nil), children[baseLower]...)
	for len(frontier) > 0 {
		c := frontier[len(frontier)-1]
		frontier = frontier[:len(frontier)-1]
		if descends[c] {
			continue
		}
		descends[c] = true
		frontier = append(frontier, children[c]...)
	}

	// Any chain commit NOT in the descendant set was pulled in from outside the base
	// lineage (an off-base merge parent, or an independent root and its off-base
	// descendants reachable only through a merge). Refuse the first such commit — it
	// carries content not authored forward from the base.
	for commit := range chain {
		if !descends[commit] {
			return refuse(fmt.Sprintf(
				"commit %s in the contribution chain does not descend from the base (pulled in via a merge of an off-base lineage)", commit), commit)
		}
	}
	return nil
}

// voidFaninContribution tombstones a seat's prior staging contributions when the
// seat is requeued (RFC 0133 requeue tombstone). It marks every non-voided staging
// row for the seat at an attempt BELOW the new attempt as 'voided' (so it can never
// satisfy staged.attempt = jobs.attempt even if a later attempt collides) and
// renames the staging ref into refs/striatum/voided/… so recovery can EXPLAIN the
// exclusion. The live-seal JOIN already makes a stale attempt structurally
// invisible; the tombstone makes the exclusion LEGIBLE rather than implicit.
func voidFaninContribution(ctx context.Context, runner db.TxRunner, repoRoot, repositoryID, barrierID, runID, workflowJobID string, newAttempt int, reason string) error {
	rows, err := queryRows(ctx, runner, `
		SELECT attempt, staging_ref
		  FROM striatumd.barrier_staged_contributions
		 WHERE repository_id = $1 AND barrier_id = $2 AND workflow_job_id = $3
		   AND status = 'staged' AND attempt < $4`,
		repositoryID, barrierID, workflowJobID, newAttempt)
	if err != nil {
		return err
	}
	for _, r := range rows {
		attempt := intValue(r["attempt"])
		stagedRef := strings.TrimSpace(fmt.Sprint(r["staging_ref"]))
		// Rename the staging ref into the voided namespace (best-effort witness).
		if stagedRef != "" {
			if sha, err := gitRevParseCommit(ctx, repoRoot, stagedRef); err == nil {
				vref := voidedRef(runID, workflowJobID, attempt)
				_, _, _ = integrateGit(ctx, repoRoot, "update-ref", vref, sha)
				_, _, _ = integrateGit(ctx, repoRoot, "update-ref", "-d", stagedRef)
			}
		}
	}
	return runner.Exec(ctx, `
		UPDATE striatumd.barrier_staged_contributions
		   SET status = 'voided', voided_at = now(), void_reason = $5
		 WHERE repository_id = $1 AND barrier_id = $2 AND workflow_job_id = $3
		   AND status = 'staged' AND attempt < $4`,
		repositoryID, barrierID, workflowJobID, newAttempt, reason)
}

// assembleFaninBarrier folds every live-staged sibling contribution onto the
// frozen tip in canonical workflow_job_id order, producing ONE deterministic
// assembled tree — the deferred post-completion join (RFC 0133 Slice 3, the part
// P1 ships as the opt-in mechanism without yet promoting it to a barrier_assembly
// job type). It reuses the EXACT merge-tree/commit-tree plumbing the D206
// per-completion path uses (mergeTreeWriteTree → commit-tree), so the assembled
// tree is byte-identical to the per-completion result for disjoint write scopes —
// the property TestFaninBarrierSameFinalTreeAsPerCompletion asserts before any
// workflow flips to the barrier.
//
// It returns the assembled tree sha, the resulting commit sha, and the ordered
// in-edges it folded (for the manifest). It NEVER advances the run branch ref by
// itself; the caller (the opt-in barrier_assembly path, P2) does the CAS
// update-ref under the lock. P1 keeps the assembly OUT of the default completion
// path — the default stays D206 per-completion.
func assembleFaninBarrier(ctx context.Context, runner db.TxRunner, repoRoot, repositoryID, barrierID string) (treeSHA, commitSHA string, edges []faninManifestInEdge, err error) {
	fp, err := loadFaninFreezePoint(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return "", "", nil, err
	}
	edges, err = faninBarrierManifest(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return "", "", nil, err
	}
	// Fold each live-staged sibling commit onto the frozen tip, in canonical
	// workflow_job_id order (edges are already sorted by entity_id). Quarantined /
	// unstaged seats contribute no commit (their gap is recorded in the manifest).
	tip := fp.FrozenTipSHA
	tree, err := gitTreeOf(ctx, repoRoot, tip)
	if err != nil {
		return "", "", nil, err
	}
	for _, edge := range edges {
		if edge.Status != "staged_live" || edge.CommitSHA == "" {
			continue
		}
		// If the staged commit already includes the current tip, fast-forward.
		if ok, err := gitIsAncestor(ctx, repoRoot, tip, edge.CommitSHA); err != nil {
			return "", "", nil, err
		} else if ok {
			tip = edge.CommitSHA
			tree, err = gitTreeOf(ctx, repoRoot, tip)
			if err != nil {
				return "", "", nil, err
			}
			continue
		}
		stdout, stderr, exit, err := mergeTreeWriteTree(ctx, repoRoot, tip, edge.CommitSHA)
		if err != nil {
			return "", "", nil, err
		}
		if exit != 0 {
			conflicts := parseMergeTreeConflicts(stdout)
			real := filterRealFanInConflicts(ctx, repoRoot, tip, edge.CommitSHA, conflicts)
			if len(real) > 0 {
				return "", "", nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
					"fan-in barrier assembly of seat %s conflicts in %d path(s): %s. Parallel fan-in lanes must use disjoint write scopes (RFC 0101).",
					edge.EntityID, len(real), strings.Join(real, ", ")),
					map[string]any{"conflicting_paths": real, "workflow_job_id": edge.EntityID})
			}
			detail := strings.TrimSpace(stdout + "\n" + stderr)
			return "", "", nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf(
				"fan-in barrier assembly merge-tree for seat %s exited %d with no real content conflict: %s", edge.EntityID, exit, detail), nil)
		}
		newTree := firstLine(stdout)
		if !isFullGitSHA(newTree) {
			return "", "", nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in barrier assembly produced invalid tree oid %q for seat %s", newTree, edge.EntityID), nil)
		}
		// For a recoverable base drift (#353) the staged commit did not descend from
		// the frozen tip; it evolved off a moved run-branch tip and was 3-way-merged
		// here. Record the recoverable leg in the assembly commit message — the staged
		// commit is already the extra commit-tree parent, so its evolved-base lineage
		// is anchored in the assembled history (an extra-parent leg, not a CAS wedge).
		msg := fmt.Sprintf("striatum fan-in barrier: assemble seat %s (attempt %d)", edge.EntityID, edge.Seal)
		if edge.BaseDriftOnto != "" {
			msg += fmt.Sprintf(" [recovered base drift: rebased onto evolved base %s]", edge.BaseDriftOnto)
		}
		// Pin the assembly commit to a deterministic author/committer timestamp so
		// crash-recovery re-assembly (assembleFaninBarrier called a second time from the
		// same frozen contributions) produces the byte-identical commit — and therefore
		// the identical commit SHA — as the original. Without this pin, git uses the
		// wall clock, so the re-assembly commit has a different timestamp and a
		// different SHA, causing the crash-recovery mismatch check in runBarrierAssembly
		// to fire erroneously (flaky: TestBarrierAssemblyCrashMidAssemblyResumes, #447).
		// The epoch "0 +0000" (1970-01-01T00:00:00 UTC) is a fixed well-known sentinel
		// that is reproducible from persisted barrier state on BOTH the original and
		// recovery paths (they both call this function with the same inputs).
		deterministicDate := "@0 +0000"
		assemblyEnv := append(os.Environ(),
			"GIT_AUTHOR_NAME=striatum-fanin",
			"GIT_AUTHOR_EMAIL=fanin@striatum.local",
			"GIT_AUTHOR_DATE="+deterministicDate,
			"GIT_COMMITTER_NAME=striatum-fanin",
			"GIT_COMMITTER_EMAIL=fanin@striatum.local",
			"GIT_COMMITTER_DATE="+deterministicDate,
		)
		cout, cexit, err := runGitWithEnv(ctx, repoRoot, assemblyEnv,
			"commit-tree", newTree, "-p", tip, "-p", edge.CommitSHA, "-m", msg)
		if err != nil {
			return "", "", nil, err
		}
		if cexit != 0 {
			return "", "", nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in barrier assembly commit-tree failed for seat %s: %s", edge.EntityID, strings.TrimSpace(cout)), nil)
		}
		tip = firstLine(cout)
		if !isFullGitSHA(tip) {
			return "", "", nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("fan-in barrier assembly commit-tree produced invalid commit oid %q for seat %s", tip, edge.EntityID), nil)
		}
		tree = newTree
	}
	return tree, tip, edges, nil
}

// gitMergeBase returns the best common ancestor of two commits, or "" when their
// histories are disjoint (no common ancestor). A non-empty result is the recoverable
// drift base the assembly folds a drifted contribution against (RFC 0133 #353).
func gitMergeBase(ctx context.Context, repoRoot, a, b string) (string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	if exit != 0 {
		// `git merge-base` exits 1 with no output when the histories are disjoint;
		// any other non-zero is a real failure.
		if strings.TrimSpace(out) == "" {
			return "", nil
		}
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("merge-base %s %s failed: %s", a, b, strings.TrimSpace(out)), nil)
	}
	base := firstLine(out)
	if base == "" {
		return "", nil
	}
	if !isFullGitSHA(base) {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("merge-base %s %s produced invalid oid %q", a, b, base), nil)
	}
	return base, nil
}

// gitRootCommits returns every parentless (root) commit reachable from commit. A
// root reachable from a staged contribution but NOT from the frozen tip is a foreign
// lineage smuggled in via a merge — the contamination signal that distinguishes a
// recoverable base drift from a contaminated base (RFC 0133 #353).
func gitRootCommits(ctx context.Context, repoRoot, commit string) ([]string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-list", "--max-parents=0", commit)
	if err != nil {
		return nil, err
	}
	if exit != 0 {
		return nil, rpc.NewError("git_commit_apply_failed", fmt.Sprintf("could not enumerate root commits of %q", commit), nil)
	}
	roots := []string{}
	for _, line := range strings.Split(out, "\n") {
		sha := strings.TrimSpace(line)
		if isFullGitSHA(sha) {
			roots = append(roots, sha)
		}
	}
	return roots, nil
}

// gitTreeOf returns the tree oid of a commit.
func gitTreeOf(ctx context.Context, repoRoot, commit string) (string, error) {
	out, exit, err := integrateGit(ctx, repoRoot, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return "", err
	}
	if exit != 0 {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("could not resolve tree of %q", commit), nil)
	}
	tree := firstLine(out)
	if !isFullGitSHA(tree) {
		return "", rpc.NewError("git_commit_apply_failed", fmt.Sprintf("invalid tree oid %q for %q", tree, commit), nil)
	}
	return tree, nil
}

// loadFaninFreezePoint reads the immutable freeze record for a barrier. RFC 0138: it
// also reads the sealed-gap tolerance columns (migration 0039) when present; on a
// deployment BEHIND on 0039 they are absent and the opt-in defaults to off (Option A).
func loadFaninFreezePoint(ctx context.Context, runner any, repositoryID, barrierID string) (faninFreezePoint, error) {
	// COALESCE the sealed-gap columns to their defaults so the projection is stable; a
	// deployment behind on 0039 lacks the columns, so select them only when present.
	gapCols := "false AS fanin_tolerates_sealed_gap, 0 AS max_sealed_gaps"
	if faninSealedGapColumnsPresent(ctx, runner) {
		gapCols = "COALESCE(fanin_tolerates_sealed_gap, false) AS fanin_tolerates_sealed_gap, COALESCE(max_sealed_gaps, 0) AS max_sealed_gaps"
	}
	rows, err := queryRows(ctx, runner, `
		SELECT barrier_id, run_id, downstream_workflow_job_id,
		       frozen_tip_sha, COALESCE(frozen_tip_tree_sha,'') AS frozen_tip_tree_sha,
		       declared_sibling_job_ids,
		       `+gapCols+`
		  FROM striatumd.fanin_freeze_points
		 WHERE repository_id = $1 AND barrier_id = $2`,
		repositoryID, barrierID)
	if err != nil {
		return faninFreezePoint{}, err
	}
	if len(rows) == 0 {
		return faninFreezePoint{}, rpc.NewError("invalid_transition", fmt.Sprintf("no fan-in freeze point for barrier %q", barrierID), nil)
	}
	r := rows[0]
	declared := []string{}
	switch v := r["declared_sibling_job_ids"].(type) {
	case []any:
		for _, item := range v {
			declared = append(declared, fmt.Sprint(item))
		}
	case string:
		_ = json.Unmarshal([]byte(v), &declared)
	case []byte:
		_ = json.Unmarshal(v, &declared)
	}
	return faninFreezePoint{
		BarrierID:               fmt.Sprint(r["barrier_id"]),
		RunID:                   fmt.Sprint(r["run_id"]),
		DownstreamWorkflowJobID: fmt.Sprint(r["downstream_workflow_job_id"]),
		FrozenTipSHA:            fmt.Sprint(r["frozen_tip_sha"]),
		FrozenTipTreeSHA:        fmt.Sprint(r["frozen_tip_tree_sha"]),
		DeclaredSiblingJobIDs:   declared,
		ToleratesSealedGap:      boolValue(r["fanin_tolerates_sealed_gap"]),
		MaxSealedGaps:           intValue(r["max_sealed_gaps"]),
	}, nil
}

// faninManifestInEdge is one row of the join_manifest.v1 in_edges list (P0's
// contract): the SEALED contribution each declared seat joined at.
type faninManifestInEdge struct {
	EntityID   string `json:"entity_id"`
	Seal       int    `json:"seal"`
	Status     string `json:"status"` // staged_live | quarantined | terminal_gap (RFC 0138 sealed gap)
	CommitSHA  string `json:"commit_sha,omitempty"`
	StagingRef string `json:"staging_ref,omitempty"`
	DamageCode string `json:"damage_code,omitempty"`
	// BaseDriftOnto is the evolved base a RECOVERABLE base-drift contribution was
	// rebased onto at assembly (#353), empty for a direct-descendant contribution.
	// The assembly records it as the extra commit-tree parent leg and stamps it into
	// the assembly commit message, so the recovered drift is auditable in the join.
	BaseDriftOnto string `json:"base_drift_onto,omitempty"`
}

// faninBarrierManifest snapshots, under the held advisory lock, the per-seat join
// provenance the barrier fired on — exactly what the join_manifest.v1 artifact
// records. The seal each contribution joined at is the load-bearing field: it is
// the seat's LIVE attempt, so a superseded-seal contribution is provably absent.
func faninBarrierManifest(ctx context.Context, runner db.TxRunner, repositoryID, barrierID string) ([]faninManifestInEdge, error) {
	fp, err := loadFaninFreezePoint(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return nil, err
	}
	// RFC 0138: resolve which outstanding seats were admitted as sealed terminal gaps,
	// so the manifest records them as status=terminal_gap (NOT silently dropped) with a
	// damage_code — the completeness-honesty record a downstream gate can refuse.
	sealedGaps, err := resolveFaninSealedGapSeats(ctx, runner, repositoryID, barrierID)
	if err != nil {
		return nil, err
	}
	sealedGapSet := make(map[string]bool, len(sealedGaps))
	for _, s := range sealedGaps {
		sealedGapSet[s] = true
	}
	edges := make([]faninManifestInEdge, 0, len(fp.DeclaredSiblingJobIDs))
	for _, seat := range fp.DeclaredSiblingJobIDs {
		edge, err := faninManifestEdgeForSeat(ctx, runner, repositoryID, barrierID, fp.RunID, seat, sealedGapSet[seat])
		if err != nil {
			return nil, err
		}
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].EntityID < edges[j].EntityID })
	return edges, nil
}

func faninManifestEdgeForSeat(ctx context.Context, runner db.TxRunner, repositoryID, barrierID, runID, seat string, sealedGap bool) (faninManifestInEdge, error) {
	// Quarantined (canceled + quarantine marker) seats are terminal gaps.
	jobRows, err := queryRows(ctx, runner, `
		SELECT state, COALESCE(write_scope_json->>'quarantined','') AS quarantined
		  FROM striatumd.jobs
		 WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3`,
		repositoryID, runID, seat)
	if err != nil {
		return faninManifestInEdge{}, err
	}
	quarantined := false
	for _, jr := range jobRows {
		if fmt.Sprint(jr["state"]) == "canceled" && fmt.Sprint(jr["quarantined"]) == "true" {
			quarantined = true
		}
	}
	if quarantined {
		return faninManifestInEdge{EntityID: seat, Seal: 0, Status: "quarantined", DamageCode: "seat_quarantined"}, nil
	}
	// RFC 0138: a seat admitted as a SEALED GAP (opted-in barrier + provably-dead seat
	// + within budget) is a terminal gap recorded LOUDLY — the join fired WITHOUT this
	// required leg, so it carries the required_seat_unrecoverable damage_code and is
	// NEVER recorded as staged_live. This is checked BEFORE the live-staged lookup
	// because a sealed-gap seat is outstanding (no live-seal staged contribution).
	if sealedGap {
		return faninManifestInEdge{EntityID: seat, Seal: 0, Status: "terminal_gap", DamageCode: faninSealedGapDamageCode}, nil
	}
	// The live-seal staged contribution: the highest-attempt non-voided,
	// non-recovery staging row, joined on the seat's live attempt. base_drift_onto_sha
	// is non-NULL only for a recovered base-drift leg (#353).
	rows, err := queryRows(ctx, runner, `
		SELECT s.attempt, s.commit_sha, s.staging_ref,
		       COALESCE(s.base_drift_onto_sha,'') AS base_drift_onto_sha
		  FROM striatumd.barrier_staged_contributions s
		  JOIN (
		        SELECT MAX(attempt) AS attempt
		          FROM striatumd.jobs
		         WHERE repository_id = $1 AND run_id = $2 AND workflow_job_id = $3
		           AND state = 'completed'
		       ) live ON live.attempt = s.attempt
		 WHERE s.repository_id = $1 AND s.barrier_id = $4 AND s.workflow_job_id = $3
		   AND s.status = 'staged'
		   AND s.staging_ref NOT LIKE 'refs/striatum/recovery/%'`,
		repositoryID, runID, seat, barrierID)
	if err != nil {
		return faninManifestInEdge{}, err
	}
	if len(rows) == 0 {
		// Outstanding seat (not live-staged, not quarantined). The manifest is only
		// snapshotted on a READY barrier, so this should not occur on a fired
		// barrier; record it honestly as an unsatisfied seat so a premature snapshot
		// is legible rather than silently dropping the seat.
		return faninManifestInEdge{EntityID: seat, Seal: 0, Status: "quarantined", DamageCode: "seat_not_yet_staged"}, nil
	}
	r := rows[0]
	return faninManifestInEdge{
		EntityID:      seat,
		Seal:          intValue(r["attempt"]),
		Status:        "staged_live",
		CommitSHA:     fmt.Sprint(r["commit_sha"]),
		StagingRef:    fmt.Sprint(r["staging_ref"]),
		BaseDriftOnto: strings.TrimSpace(fmt.Sprint(r["base_drift_onto_sha"])),
	}, nil
}
