package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// escalationNotifyURLEnvName mirrors the mutations package's unexported
// STRIATUM_ESCALATION_NOTIFY_URL constant (the opt-in switch for the
// post-commit escalation notifier).
const escalationNotifyURLEnvName = "STRIATUM_ESCALATION_NOTIFY_URL"

// poisonSweep returns an ActiveRunSweep whose per-run unit always fails for
// runID, driving the poison-run breaker toward its trip threshold.
func poisonSweep(runner db.Runner, runID string) ActiveRunSweep {
	return ActiveRunSweep{
		Runner: runner,
		Author: "test",
		sweepRun: func(_ context.Context, _ db.Runner, _ string, sweptRunID string, _ string) (map[string]any, error) {
			if sweptRunID == runID {
				return nil, errors.New("synthetic per-run recovery panic/error")
			}
			return map[string]any{"status": "ok"}, nil
		},
	}
}

// TestRecoverySweepBreakerTripNotifiesEscalationOnce: when the poison-run
// breaker trips (after recoverySweepTripThreshold consecutive degraded
// sweeps), the opt-in escalation notifier fires EXACTLY ONCE, strictly after
// the trip transaction committed, with the ID-only notification payload. The
// degraded sweeps before the trip and the sweeps after it (the run is
// needs_operator and excluded from the active-run set) must not notify.
func TestRecoverySweepBreakerTripNotifiesEscalationOnce(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_sweep_trip_notify"
	poisonRunID := "run_sweep_trip_notify_poison"
	seedRecoverySweepTripFixture(t, ctx, runner, repoID, poisonRunID)

	var requests atomic.Int64
	payloads := make(chan map[string]any, 4)
	handlerErrs := make(chan error, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost {
			handlerErrs <- fmt.Errorf("method = %s, want POST", r.Method)
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			handlerErrs <- fmt.Errorf("decode payload: %w", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		// Post-commit proof: the trip's escalation row must already be durable
		// and visible on an independent connection when the notification fires.
		var committed int
		if err := runner.QueryRow(r.Context(), `
			SELECT COUNT(*)::int FROM striatumd.escalation_inbox
			 WHERE repository_id = $1 AND run_id = $2
			   AND blocker_kind = 'recovery_exhausted'
			   AND payload_json->>'source' = $3`,
			repoID, poisonRunID, recoverySweepTripSource).Scan(&committed); err != nil {
			handlerErrs <- fmt.Errorf("read committed trip escalation from handler: %w", err)
			http.Error(w, "query", http.StatusInternalServerError)
			return
		}
		if committed != 1 {
			handlerErrs <- fmt.Errorf("handler saw %d committed trip escalations, want 1 (notify must be post-commit)", committed)
			http.Error(w, "not committed", http.StatusConflict)
			return
		}
		payloads <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	t.Setenv(escalationNotifyURLEnvName, server.URL+"/notify")

	sweep := poisonSweep(runner, poisonRunID)
	for i := 0; i < recoverySweepTripThreshold; i++ {
		if _, err := sweep.SweepOnce(ctx); err != nil {
			t.Fatalf("sweep tick %d: %v", i+1, err)
		}
		if i < recoverySweepTripThreshold-1 {
			if got := requests.Load(); got != 0 {
				t.Fatalf("notifier fired %d time(s) after degraded sweep %d, want 0 before the trip", got, i+1)
			}
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("notifier requests after trip = %d, want exactly 1", got)
	}

	var payload map[string]any
	select {
	case payload = <-payloads:
	case err := <-handlerErrs:
		t.Fatalf("notifier handler error: %v", err)
	}
	select {
	case err := <-handlerErrs:
		t.Fatalf("notifier handler error: %v", err)
	default:
	}

	escalationID := recoverySweepTripEscalationID(t, ctx, runner, repoID, poisonRunID)
	if got := fmt.Sprint(payload["schema_version"]); got != "striatum.escalation_notification.v1" {
		t.Fatalf("payload schema_version = %q, want striatum.escalation_notification.v1", got)
	}
	if got := fmt.Sprint(payload["source"]); got != recoverySweepTripSource {
		t.Fatalf("payload source = %q, want %s", got, recoverySweepTripSource)
	}
	if got := fmt.Sprint(payload["repository_id"]); got != repoID {
		t.Fatalf("payload repository_id = %q, want %s", got, repoID)
	}
	if got := fmt.Sprint(payload["run_id"]); got != poisonRunID {
		t.Fatalf("payload run_id = %q, want %s", got, poisonRunID)
	}
	if got := fmt.Sprint(payload["blocker_id"]); got != escalationID {
		t.Fatalf("payload blocker_id = %q, want %s", got, escalationID)
	}
	if got := fmt.Sprint(payload["escalation_kind"]); got != recoveryExhaustedKind {
		t.Fatalf("payload escalation_kind = %q, want %s", got, recoveryExhaustedKind)
	}
	if got := fmt.Sprint(payload["blocker_kind"]); got != recoveryExhaustedKind {
		t.Fatalf("payload blocker_kind = %q, want %s", got, recoveryExhaustedKind)
	}
	if got := fmt.Sprint(payload["disposition"]); got != "sweep_trip_latch" {
		t.Fatalf("payload disposition = %q, want sweep_trip_latch", got)
	}
	// ID-only leak check: the sweep's raw error text and the durable payload
	// must never travel in the notification.
	for _, forbidden := range []string{"last_error", "description", "payload_json", "suggested_operator_actions", "transcript", "secret", "token", "authorization"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("payload included forbidden field %q: %#v", forbidden, payload)
		}
	}

	// Exactly-once: a further sweep sees the run needs_operator (excluded from
	// the active-run set) and must not re-notify.
	if _, err := sweep.SweepOnce(ctx); err != nil {
		t.Fatalf("post-trip sweep: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("notifier requests after post-trip sweep = %d, want still 1", got)
	}
}

// TestRecoverySweepBreakerTripSurvivesNotifierFailure: a failing notifier
// endpoint (503 on every request) must not roll back or block the trip — the
// sweep returns no error, the run still latches to needs_operator, and the
// recovery_exhausted escalation row is durable.
func TestRecoverySweepBreakerTripSurvivesNotifierFailure(t *testing.T) {
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoID := "repo_sweep_trip_notify_failure"
	poisonRunID := "run_sweep_trip_notify_failure_poison"
	seedRecoverySweepTripFixture(t, ctx, runner, repoID, poisonRunID)

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "notifier down", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	t.Setenv(escalationNotifyURLEnvName, server.URL+"/notify")

	sweep := poisonSweep(runner, poisonRunID)
	for i := 0; i < recoverySweepTripThreshold; i++ {
		if _, err := sweep.SweepOnce(ctx); err != nil {
			t.Fatalf("sweep tick %d should ignore notifier failure: %v", i+1, err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("notifier requests = %d, want 1", got)
	}
	if got := recoverySweepRunState(t, ctx, runner, repoID, poisonRunID); got != "needs_operator" {
		t.Fatalf("poison run state = %q, want needs_operator despite failing notifier", got)
	}
	if escalationID := recoverySweepTripEscalationID(t, ctx, runner, repoID, poisonRunID); escalationID == "" {
		t.Fatal("trip escalation row missing after notifier failure")
	}
	count, state := recoverySweepCursorBreaker(t, ctx, runner, repoID, poisonRunID)
	if count != recoverySweepTripThreshold || state != "tripped" {
		t.Fatalf("breaker cursor = count %d state %q, want count %d state tripped", count, state, recoverySweepTripThreshold)
	}
}
