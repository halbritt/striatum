package reads

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestEscalationResolveMarksBlockerAndEmitsEvent(t *testing.T) {
	runner := newEscalationResolveFake("open")
	response, err := HandleEscalationResolve(context.Background(), runner, rpc.Envelope{Params: map[string]any{
		"repository_id":   "repo_1",
		"escalation_id":   "blk_1",
		"decision_id":     "dec_1",
		"resolution_note": "continue with documented constraint",
	}})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if runner.tx.commits != 1 || runner.tx.rollbacks != 0 {
		t.Fatalf("transaction commits=%d rollbacks=%d", runner.tx.commits, runner.tx.rollbacks)
	}
	if got := runner.blockerState; got != "resolved" {
		t.Fatalf("blocker state = %q, want resolved", got)
	}
	resolution := objectOrEmpty(runner.blockerPayload["escalation_resolution"])
	if resolution["decision_id"] != "dec_1" || resolution["resolution_note"] != "continue with documented constraint" {
		t.Fatalf("resolution payload = %#v", resolution)
	}
	if runner.eventType != "escalation.resolved" {
		t.Fatalf("event type = %q", runner.eventType)
	}
	if runner.eventJobID != "job_1" {
		t.Fatalf("event job id = %#v", runner.eventJobID)
	}
	if runner.eventPayload["escalation_id"] != "blk_1" || runner.eventPayload["decision_id"] != "dec_1" {
		t.Fatalf("event payload = %#v", runner.eventPayload)
	}
	escalation := response["escalation"].(map[string]any)
	if response["status"] != "resolved" || escalation["state"] != "resolved" {
		t.Fatalf("response = %#v", response)
	}
	if objectOrEmpty(escalation["payload"])["escalation_resolution"] == nil {
		t.Fatalf("response payload missing resolution: %#v", escalation["payload"])
	}
}

// #505: when escalation.resolve leaves the run running with claimable work, the
// response must advertise the `run drive --run-id` re-arm verb so the detached
// auto-driver (which exited when the run hit needs_operator) is restarted
// instead of the run silently stalling. Mirrors checkpoint.resolve's hint.
func TestEscalationResolveAdvertisesReArmHintWhenDrivable(t *testing.T) {
	runner := newEscalationResolveFake("open")
	runner.runStateAfter = "running"
	runner.hasClaimable = true
	response, err := HandleEscalationResolve(context.Background(), runner, rpc.Envelope{Params: map[string]any{
		"repository_id": "repo_1",
		"escalation_id": "blk_1",
	}})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if response["run_state"] != "running" {
		t.Fatalf("run_state = %#v, want running", response["run_state"])
	}
	next, _ := response["next_actions"].([]string)
	found := false
	for _, action := range next {
		if strings.HasPrefix(action, "run drive --run-id run_1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("next_actions missing re-arm hint: %#v", response["next_actions"])
	}
}

// #505: a run that is NOT drivable after the resolve (no claimable work, or
// still terminal/paused) must NOT get the re-arm hint.
func TestEscalationResolveOmitsReArmHintWhenNotDrivable(t *testing.T) {
	runner := newEscalationResolveFake("open")
	runner.runStateAfter = "running"
	runner.hasClaimable = false
	response, err := HandleEscalationResolve(context.Background(), runner, rpc.Envelope{Params: map[string]any{
		"repository_id": "repo_1",
		"escalation_id": "blk_1",
	}})
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if _, ok := response["next_actions"]; ok {
		t.Fatalf("did not expect a re-arm hint when there is no claimable work: %#v", response["next_actions"])
	}
}

func TestEscalationResolveRejectsClosedEscalation(t *testing.T) {
	runner := newEscalationResolveFake("resolved")
	_, err := HandleEscalationResolve(context.Background(), runner, rpc.Envelope{Params: map[string]any{
		"repository_id": "repo_1",
		"blocker_id":    "blk_1",
	}})
	var rpcErr *rpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != "invalid_transition" {
		t.Fatalf("error = %v, want invalid_transition", err)
	}
	if runner.tx.commits != 0 || runner.tx.rollbacks != 1 {
		t.Fatalf("transaction commits=%d rollbacks=%d", runner.tx.commits, runner.tx.rollbacks)
	}
	if len(runner.tx.execSQL) != 0 {
		t.Fatalf("unexpected execs: %#v", runner.tx.execSQL)
	}
}

type escalationResolveFake struct {
	tx              *escalationResolveFakeTx
	blockerState    string
	blockerPayload  map[string]any
	blockerResolved any
	eventType       string
	eventJobID      any
	eventPayload    map[string]any
	// #505 re-arm-hint controls: the run's state read AFTER the resolve, and
	// whether it has any queued/blocked job. Defaults model the common
	// "still-running, no claimable work" case (no hint).
	runStateAfter string
	hasClaimable  bool
}

func newEscalationResolveFake(state string) *escalationResolveFake {
	runner := &escalationResolveFake{
		blockerState:   state,
		blockerPayload: map[string]any{"existing": "kept"},
		runStateAfter:  "running",
	}
	runner.tx = &escalationResolveFakeTx{parent: runner}
	return runner
}

func (r *escalationResolveFake) Exec(context.Context, string, ...any) error {
	return errors.New("unexpected root exec")
}

func (r *escalationResolveFake) QueryRow(context.Context, string, ...any) db.Row {
	return escalationFakeRow{err: pgx.ErrNoRows}
}

func (r *escalationResolveFake) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected query scalar")
}

func (r *escalationResolveFake) BeginTx(context.Context) (db.TxRunner, error) {
	return r.tx, nil
}

func (r *escalationResolveFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return projectionRows(r), nil
}

type escalationResolveFakeTx struct {
	parent    *escalationResolveFake
	execSQL   []string
	commits   int
	rollbacks int
}

func (tx *escalationResolveFakeTx) Exec(_ context.Context, sql string, args ...any) error {
	tx.execSQL = append(tx.execSQL, sql)
	switch {
	case strings.Contains(sql, "UPDATE striatumd.blockers"):
		tx.parent.blockerResolved = args[0]
		tx.parent.blockerPayload = objectOrEmpty(args[1])
		tx.parent.blockerState = "resolved"
	case strings.Contains(sql, "INSERT INTO striatumd.events"):
		tx.parent.eventType, _ = args[3].(string)
		tx.parent.eventJobID = args[5]
		tx.parent.eventPayload = objectOrEmpty(args[9])
	}
	return nil
}

func (tx *escalationResolveFakeTx) QueryRow(_ context.Context, sql string, _ ...any) db.Row {
	if strings.Contains(sql, "repo_event_chain_heads") {
		return escalationFakeRow{values: []any{nil}}
	}
	if strings.Contains(sql, "nextval") {
		return escalationFakeRow{values: []any{int64(77)}}
	}
	return escalationFakeRow{err: pgx.ErrNoRows}
}

func (tx *escalationResolveFakeTx) QueryScalar(context.Context, string, ...any) (string, error) {
	return "", errors.New("unexpected query scalar")
}

func (tx *escalationResolveFakeTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "striatumd.artifacts") {
		return &fakeRows{
			fields: []string{"artifact_id", "run_id", "job_id", "session_id", "logical_name"},
			rows: [][]any{{
				"art_decision", "run_1", nil, nil, "dec_1",
			}},
		}, nil
	}
	// RFC 0101 Phase 4: the `UPDATE striatumd.runs ... RETURNING run_id` that
	// clears needs_operator. This fake does not model a needs_operator run, so
	// the guarded UPDATE affects zero rows (the common case: resolving an
	// escalation on a still-running run). The live-PG mutations tests cover the
	// flip path. Returning no rows means run.resumed is not emitted here.
	if strings.Contains(sql, "UPDATE striatumd.runs") {
		return &fakeRows{fields: []string{"run_id"}, rows: [][]any{}}, nil
	}
	// #505: the post-resolve run-state read used to build the re-arm hint.
	if strings.Contains(sql, "SELECT state, paused_at FROM striatumd.runs") {
		return &fakeRows{
			fields: []string{"state", "paused_at"},
			rows:   [][]any{{tx.parent.runStateAfter, nil}},
		}, nil
	}
	// #505: the claimable-work probe used to build the re-arm hint.
	if strings.Contains(sql, "FROM striatumd.jobs") && strings.Contains(sql, "queued") {
		rows := [][]any{}
		if tx.parent.hasClaimable {
			rows = [][]any{{int64(1)}}
		}
		return &fakeRows{fields: []string{"?column?"}, rows: rows}, nil
	}
	return &fakeRows{
		fields: []string{"blocker_id", "run_id", "job_id", "state", "payload_json"},
		rows: [][]any{{
			"blk_1",
			"run_1",
			"job_1",
			tx.parent.blockerState,
			tx.parent.blockerPayload,
		}},
	}, nil
}

func (tx *escalationResolveFakeTx) Commit(context.Context) error {
	tx.commits++
	return nil
}

func (tx *escalationResolveFakeTx) Rollback(context.Context) error {
	tx.rollbacks++
	return nil
}

type escalationFakeRow struct {
	values []any
	err    error
}

func (r escalationFakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		if i >= len(r.values) {
			break
		}
		switch target := dest[i].(type) {
		case **string:
			if r.values[i] == nil {
				*target = nil
			} else {
				text := r.values[i].(string)
				*target = &text
			}
		case *int64:
			*target = r.values[i].(int64)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}

func projectionRows(r *escalationResolveFake) pgx.Rows {
	return &fakeRows{
		fields: []string{
			"escalation_id", "blocker_id", "run_id", "job_id", "workflow_job_id",
			"session_id", "session_role_id", "session_lane_id", "severity", "class",
			"description", "state", "created_at", "resolved_at", "payload_json",
			"linked_artifact_id", "linked_repo_path", "linked_content_sha256",
		},
		rows: [][]any{{
			"blk_1", "blk_1", "run_1", "job_1", nil,
			nil, nil, nil, "error", "missing_authority",
			"needs principal decision", r.blockerState, "2026-05-17T00:00:00Z", r.blockerResolved, r.blockerPayload,
			nil, nil, nil,
		}},
	}
}

type fakeRows struct {
	fields []string
	rows   [][]any
	index  int
	err    error
}

func (r *fakeRows) Close() {}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	fields := make([]pgconn.FieldDescription, 0, len(r.fields))
	for _, name := range r.fields {
		fields = append(fields, pgconn.FieldDescription{Name: name})
	}
	return fields
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		r.Close()
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if len(dest) == 1 {
		if scanner, ok := dest[0].(interface {
			ScanRow(pgx.Rows) error
		}); ok {
			return scanner.ScanRow(r)
		}
	}
	values, err := r.Values()
	if err != nil {
		return err
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *any:
			*target = values[i]
		default:
			return errors.New("unsupported fakeRows scan target")
		}
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.index == 0 || r.index > len(r.rows) {
		return nil, errors.New("Values called without current row")
	}
	return r.rows[r.index-1], nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}
