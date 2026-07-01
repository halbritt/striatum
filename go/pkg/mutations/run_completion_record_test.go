package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0118 P1-5 (GH #240): every terminal run transition freezes a durable,
// machine-readable run_completion_record INSIDE the terminal transaction,
// BEFORE closeRemainingSessions tears the lanes down — so the record captures
// the last-live state instead of the post-teardown emptiness the run_45aa8852
// retrospective hit. Tamper evidence: the record's sha256 is anchored in the
// append-only terminal event (operator decision #3).

func readCompletionRecord(t *testing.T, ctx context.Context, runner db.Runner, repoID, runID string) map[string]any {
	t.Helper()
	row, err := oneRow(ctx, runner, `
		SELECT completion_record_json FROM striatumd.runs
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID)
	if err != nil {
		t.Fatalf("read completion record: %v", err)
	}
	if row["completion_record_json"] == nil {
		t.Fatalf("completion_record_json is NULL, want a frozen record")
	}
	return asMap(row["completion_record_json"])
}

// The incident path: an operator-canceled run snapshots its sessions while
// they are still live — the record holds the attested lane state and the
// imminent close reason, and the run.canceled event anchors the record hash.
func TestRunCancelWritesCompletionRecord(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_cancel_record"
	runID := "run_" + repoID
	sessionID := "sess_lane_" + repoID
	jobID := "job_build_" + repoID
	intgSeedRepo(t, ctx, runner, repoID)
	intgSeedRun(t, ctx, runner, repoID, runID, map[string]any{
		"workflow_id": "cancel_wf",
		"lanes":       map[string]any{"builder": map[string]any{}},
		"jobs":        []any{map[string]any{"id": "build", "type": "draft"}},
	})
	intgSeedSession(t, ctx, runner, repoID, runID, sessionID, "builder", "builder", []string{"draft"}, "active")
	intgAttest(t, ctx, runner, repoID, runID, sessionID, "builder")
	now := time.Now().UTC()
	if err := runner.Exec(ctx, `
		INSERT INTO striatumd.jobs (
		  repository_id, job_id, run_id, workflow_job_id, attempt, state, role_id,
		  title, job_type, expected_artifacts_json, idempotency_key, created_at, started_at
		) VALUES ($1,$2,$3,'build',1,'running','builder','Build','draft','[]'::jsonb,'idem_'||$2,$4,$4)`,
		repoID, jobID, runID, now); err != nil {
		t.Fatalf("insert running job: %v", err)
	}

	if _, err := HandleRunCancel(ctx, runner, intgEnv(repoID, map[string]any{
		"run_id": runID,
		"reason": "operator_canceled",
	})); err != nil {
		t.Fatalf("run cancel: %v", err)
	}

	record := readCompletionRecord(t, ctx, runner, repoID, runID)
	if got := fmt.Sprint(record["terminal_state"]); got != "canceled" {
		t.Fatalf("record terminal_state = %q, want canceled", got)
	}
	sessions := asList(record["sessions"])
	if len(sessions) != 1 {
		t.Fatalf("record sessions = %#v, want 1 entry", record["sessions"])
	}
	session := asMap(sessions[0])
	if fmt.Sprint(session["session_id"]) != sessionID {
		t.Fatalf("record session_id = %v, want %s", session["session_id"], sessionID)
	}
	// Snapshot ran BEFORE teardown: the lane reads attested with its live
	// supervisor, and the imminent close reason is recorded.
	attestation := asMap(session["attestation"])
	if fmt.Sprint(attestation["state"]) != "attested" {
		t.Fatalf("frozen attestation = %#v, want state attested (snapshot must precede session teardown)", attestation)
	}
	if got := fmt.Sprint(session["close_reason"]); got != "run_canceled" {
		t.Fatalf("session close_reason = %q, want run_canceled (imminent close reason)", got)
	}
	// The append-only run.canceled event anchors the record hash (decision #3).
	event, err := oneRow(ctx, runner, `
		SELECT payload_json FROM striatumd.events
		 WHERE repository_id = $1 AND run_id = $2 AND event_type = 'run.canceled'
		 ORDER BY event_id DESC LIMIT 1`, repoID, runID)
	if err != nil {
		t.Fatalf("read run.canceled event: %v", err)
	}
	anchored := fmt.Sprint(asMap(event["payload_json"])["completion_record_sha256"])
	canonical, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	sum := sha256.Sum256(canonical)
	if rehash := hex.EncodeToString(sum[:]); anchored != rehash {
		t.Fatalf("anchored sha256 = %s, re-hash = %s — record and event anchor disagree", anchored, rehash)
	}
}

// The record is write-once: a terminal transition never overwrites an
// existing record (decision #3 overwrite guard).
func TestCompletionRecordIsWriteOnce(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_record_writeonce"
	runID, _, _ := seedProvenanceGateRun(t, ctx, runner, repoID,
		map[string]any{"id": "review", "type": "review"},
		map[string]any{"lane_attestation_at_record": "attested", "review_provenance_override": false})
	if err := runner.Exec(ctx, `
		UPDATE striatumd.runs SET completion_record_json = '{"seeded": true}'::jsonb
		 WHERE repository_id = $1 AND run_id = $2`, repoID, runID); err != nil {
		t.Fatalf("pre-seed record: %v", err)
	}

	driveMaybeCompleteRun(t, ctx, runner, repoID, runID)

	record := readCompletionRecord(t, ctx, runner, repoID, runID)
	if record["seeded"] != true || record["terminal_state"] != nil {
		t.Fatalf("record = %#v, want the pre-seeded record preserved (write-once)", record)
	}
}

// A clean completion freezes the P0-3 ledger, the completion mode, and the
// frozen verdict stamps into the record.
func TestRunCompletionWritesRecordWithLedger(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_record_ledger"
	runID, jobID, _ := seedProvenanceGateRun(t, ctx, runner, repoID,
		map[string]any{"id": "review", "type": "review", "fresh_session_required": true},
		map[string]any{
			"lane_attestation_at_record": "attested",
			"review_provenance_override": false,
			"supervisor_id_at_record":    "sup_seeded",
		})

	driveMaybeCompleteRun(t, ctx, runner, repoID, runID)

	record := readCompletionRecord(t, ctx, runner, repoID, runID)
	if got := fmt.Sprint(record["terminal_state"]); got != "completed" {
		t.Fatalf("record terminal_state = %q, want completed", got)
	}
	if got := fmt.Sprint(record["completion_mode"]); got != "lanes_attested" {
		t.Fatalf("record completion_mode = %q, want lanes_attested", got)
	}
	if len(asList(record["provenance_gate"])) != 1 {
		t.Fatalf("record provenance_gate = %#v, want 1 ledger entry", record["provenance_gate"])
	}
	verdicts := asList(record["verdicts"])
	if len(verdicts) != 1 {
		t.Fatalf("record verdicts = %#v, want 1 frozen stamp", record["verdicts"])
	}
	stamp := asMap(verdicts[0])
	if fmt.Sprint(stamp["job_id"]) != jobID || fmt.Sprint(stamp["lane_attestation_at_record"]) != "attested" {
		t.Fatalf("frozen verdict stamp = %#v, want attested stamp for %s", stamp, jobID)
	}
	if fmt.Sprint(stamp["model_identity_declared"]) != verdictModelUnknown ||
		fmt.Sprint(stamp["model_co_blindness_at_record"]) != verdictCoBlindnessUnknown {
		t.Fatalf("frozen verdict model identity = %#v, want unknown historical stamp", stamp)
	}
	gate := asMap(asList(record["provenance_gate"])[0])
	if fmt.Sprint(gate["model_identity_declared"]) != verdictModelUnknown ||
		fmt.Sprint(gate["model_co_blindness_at_record"]) != verdictCoBlindnessUnknown {
		t.Fatalf("provenance gate model identity = %#v, want unknown historical stamp", gate)
	}
	jobs := asList(record["jobs"])
	if len(jobs) != 1 || fmt.Sprint(asMap(jobs[0])["state"]) != "completed" {
		t.Fatalf("record jobs = %#v, want the completed review job", record["jobs"])
	}
}
