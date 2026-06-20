package reads

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/pgtest"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// RFC 0118 P0-1 (GH #240): run.summary surfaces each verdict's frozen
// provenance stamp so a terminal run's review provenance is auditable from
// the summary alone — no live lanehealth probe, no daemon archaeology.
func TestRunSummaryVerdictsCarryFrozenProvenanceStamp(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_stamp_summary"
	runID := "run_stamp"
	now := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)
	crSeedRun(t, ctx, runner, repoID, runID, "striatum/stamp", now, false, nil)

	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.verdicts (
		  repository_id, verdict_id, run_id, job_id, session_id, verdict,
		  rationale, created_at, posture,
		  lane_attestation_at_record, review_provenance_override,
		  review_provenance_decision_id, supervisor_id_at_record
		) VALUES ($1,'verdict_stamp',$2,$3,$4,'accept','ok',$5,'override',
		  'unattested',true,'dec_stamp','sup_stamp')`,
		repoID, runID, "job_"+runID, "sess_"+runID, now); err != nil {
		t.Fatalf("seed stamped verdict: %v", err)
	}

	result, err := HandleRunSummary(ctx, runner, rpc.Envelope{
		Params: map[string]any{"repository_id": repoID, "run_id": runID},
	})
	if err != nil {
		t.Fatalf("HandleRunSummary: %v", err)
	}
	verdicts, ok := result["verdicts"].([]map[string]any)
	if !ok || len(verdicts) != 1 {
		t.Fatalf("verdicts = %#v, want exactly one row", result["verdicts"])
	}
	row := verdicts[0]
	if got := fmt.Sprint(row["lane_attestation_at_record"]); got != "unattested" {
		t.Fatalf("lane_attestation_at_record = %q, want unattested", got)
	}
	if row["review_provenance_override"] != true {
		t.Fatalf("review_provenance_override = %v, want true", row["review_provenance_override"])
	}
	if got := fmt.Sprint(row["review_provenance_decision_id"]); got != "dec_stamp" {
		t.Fatalf("review_provenance_decision_id = %q, want dec_stamp", got)
	}
	if got := fmt.Sprint(row["supervisor_id_at_record"]); got != "sup_stamp" {
		t.Fatalf("supervisor_id_at_record = %q, want sup_stamp", got)
	}

	// RFC 0118 P0-4: run.summary surfaces the run's completion_mode and an
	// overrides[] block listing every override-cleared verdict with its
	// authorizing decision.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET state = 'completed', completion_mode = 'operator_override'
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID); err != nil {
		t.Fatalf("set completion_mode: %v", err)
	}
	result, err = HandleRunSummary(ctx, runner, rpc.Envelope{
		Params: map[string]any{"repository_id": repoID, "run_id": runID},
	})
	if err != nil {
		t.Fatalf("HandleRunSummary after completion: %v", err)
	}
	run, ok := result["run"].(map[string]any)
	if !ok {
		t.Fatalf("run block = %#v", result["run"])
	}
	if got := fmt.Sprint(run["completion_mode"]); got != "operator_override" {
		t.Fatalf("run.completion_mode = %q, want operator_override", got)
	}
	overrides, ok := result["overrides"].([]map[string]any)
	if !ok || len(overrides) != 1 {
		t.Fatalf("overrides = %#v, want exactly one override-cleared verdict", result["overrides"])
	}
	if got := fmt.Sprint(overrides[0]["review_provenance_decision_id"]); got != "dec_stamp" {
		t.Fatalf("overrides[0].review_provenance_decision_id = %q, want dec_stamp", got)
	}
}

// RFC 0118 P1-5: run.summary on a TERMINAL run reads the frozen
// run_completion_record — the sessions[] block reflects the last-live state
// captured before teardown, not the post-teardown emptiness (the
// run_45aa8852 incident read).
func TestRunSummaryTerminalRunReadsFrozenRecord(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_summary_frozen"
	runID := "run_frozen"
	now := time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)
	crSeedRun(t, ctx, runner, repoID, runID, "striatum/frozen", now, false, nil)
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'canceled', completion_record_json = $1::jsonb
		 WHERE repository_id = $2 AND run_id = $3`,
		`{"schema_version":"striatumd.run_completion_record.v1",
		  "terminal_state":"canceled",
		  "sessions":[{"session_id":"sess_frozen_lane","state":"active",
		    "close_reason":"run_canceled",
		    "attestation":{"state":"attested","supervisor_id":"sup_frozen"}}],
		  "provenance_gate":[{"workflow_job_id":"review","basis":"attested"}]}`,
		repoID, runID); err != nil {
		t.Fatalf("freeze record: %v", err)
	}

	result, err := HandleRunSummary(ctx, runner, rpc.Envelope{
		Params: map[string]any{"repository_id": repoID, "run_id": runID},
	})
	if err != nil {
		t.Fatalf("HandleRunSummary: %v", err)
	}
	sessions, ok := result["sessions"].([]any)
	if !ok || len(sessions) != 1 {
		t.Fatalf("sessions = %#v, want the frozen single-session block", result["sessions"])
	}
	frozen, _ := sessions[0].(map[string]any)
	if fmt.Sprint(frozen["session_id"]) != "sess_frozen_lane" {
		t.Fatalf("sessions[0] = %#v, want frozen sess_frozen_lane", frozen)
	}
	attestation, _ := frozen["attestation"].(map[string]any)
	if fmt.Sprint(attestation["state"]) != "attested" {
		t.Fatalf("frozen attestation = %#v, want last-live attested state", frozen["attestation"])
	}
	record, ok := result["completion_record"].(map[string]any)
	if !ok || fmt.Sprint(record["terminal_state"]) != "canceled" {
		t.Fatalf("completion_record = %#v, want the frozen record projection", result["completion_record"])
	}
	// A NON-terminal run falls back to the live sessions projection.
	liveRunID := "run_frozen_live"
	crSeedRun(t, ctx, runner, repoID, liveRunID, "striatum/frozen-live", now.Add(time.Minute), false, nil)
	liveResult, err := HandleRunSummary(ctx, runner, rpc.Envelope{
		Params: map[string]any{"repository_id": repoID, "run_id": liveRunID},
	})
	if err != nil {
		t.Fatalf("HandleRunSummary live: %v", err)
	}
	liveSessions, ok := liveResult["sessions"].([]map[string]any)
	if !ok || len(liveSessions) != 1 || fmt.Sprint(liveSessions[0]["session_id"]) != "sess_"+liveRunID {
		t.Fatalf("live sessions = %#v, want the live session row", liveResult["sessions"])
	}
}

// RFC 0118 P1-5: the evidence export gains deterministic ## Sessions /
// ## Provenance Gate / ## Operator Overrides sections.
func TestEvidenceExportRendersProvenanceSections(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_evidence_sections"
	runID := "run_evidence"
	now := time.Date(2026, 6, 10, 11, 0, 0, 0, time.UTC)
	crSeedRepo(t, ctx, runner, repoID, now)
	crSeedRun(t, ctx, runner, repoID, runID, "striatum/evidence", now, false, nil)
	repoRoot := t.TempDir()
	if err := runner.Exec(ctx, `
		UPDATE striatumd.repositories SET repo_root = $1 WHERE repository_id = $2`,
		repoRoot, repoID); err != nil {
		t.Fatalf("set repo root: %v", err)
	}
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.verdicts (
		  repository_id, verdict_id, run_id, job_id, session_id, verdict,
		  created_at, posture, lane_attestation_at_record,
		  review_provenance_override, review_provenance_decision_id
		) VALUES ($1,'verdict_evidence',$2,$3,$4,'accept',$5,'override',
		  'unattested',true,'dec_evidence')`,
		repoID, runID, "job_"+runID, "sess_"+runID, now); err != nil {
		t.Fatalf("seed override verdict: %v", err)
	}
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs
		   SET state = 'completed', completion_mode = 'operator_override',
		       completion_record_json = $1::jsonb
		 WHERE repository_id = $2 AND run_id = $3`,
		`{"terminal_state":"completed",
		  "sessions":[{"session_id":"sess_run_evidence","state":"active","close_reason":"run_completed"}],
		  "provenance_gate":[{"workflow_job_id":"review","basis":"override","override_decision_id":"dec_evidence"}],
		  "claim_verification":[
		    {"claim_id":"C1","authored_status":"VERIFIED","effective_status":"VERIFIED","verification_basis":"two_signal_sealed_receipt"},
		    {"claim_id":"C2","authored_status":"VERIFIED","effective_status":"ASSERTED","verification_basis":"receipt_seal_mismatch"}]}`,
		repoID, runID); err != nil {
		t.Fatalf("freeze record: %v", err)
	}

	result, err := HandleEvidenceExport(ctx, runner, rpc.Envelope{
		Params: map[string]any{
			"repository_id": repoID,
			"run_id":        runID,
			"path":          "docs/evidence/run_evidence.md",
		},
	})
	if err != nil {
		t.Fatalf("evidence export: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(repoRoot, "docs/evidence/run_evidence.md"))
	if err != nil {
		t.Fatalf("read exported markdown: %v", err)
	}
	text := string(body)
	for _, section := range []string{"## Sessions", "## Provenance Gate", "## Operator Overrides", "## Claim Verification"} {
		if !strings.Contains(text, section) {
			t.Fatalf("exported markdown missing %q section:\n%s", section, text)
		}
	}
	if !strings.Contains(text, "sess_run_evidence") || !strings.Contains(text, "dec_evidence") {
		t.Fatalf("sections missing frozen session / override decision:\n%s", text)
	}
	// RFC 0134: the claim-verification ledger renders authored vs. effective status
	// and the degrade basis (a VERIFIED claim that decayed to ASSERTED is legible).
	for _, want := range []string{"claim `C1`", "effective=`VERIFIED`", "basis=`two_signal_sealed_receipt`", "claim `C2`", "basis=`receipt_seal_mismatch`"} {
		if !strings.Contains(text, want) {
			t.Fatalf("claim-verification section missing %q:\n%s", want, text)
		}
	}
	if fmt.Sprint(result["status"]) != "exported" {
		t.Fatalf("status = %v, want exported", result["status"])
	}
}
