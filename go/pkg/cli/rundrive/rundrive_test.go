package rundrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func TestRunDriveReconcileIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{
		job("job_author", "author_draft", "author", "codex", "queued", 1),
		job("job_review", "review", "reviewer", "agy", "queued", 1),
	}
	driver := testDriver(fake)

	actions1, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	actions2, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 2 {
		t.Fatalf("session.register calls = %d, want 2; actions1=%#v actions2=%#v", got, actions1, actions2)
	}
	if got := fake.count("supervise.start"); got != 2 {
		t.Fatalf("supervise.start calls = %d, want 2", got)
	}
	if len(actions2) != 0 {
		t.Fatalf("second reconcile should be quiet/idempotent, got %#v", actions2)
	}
}

// TestRunDriveHonorsMaxActiveJobs (#322): with max_active_jobs:1 and five queued
// jobs on distinct lanes, ReconcileOnce issues exactly ONE supervise.start — the
// driver inherits the cap from the shared PlanLaunch predicate.
func TestRunDriveHonorsMaxActiveJobs(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.workflow = map[string]any{
		"jobs":        []any{},
		"parallelism": map[string]any{"max_active_jobs": 1},
	}
	fake.jobs = []map[string]any{
		job("j1", "w1", "author", "codex", "queued", 1),
		job("j2", "w2", "reviewer", "agy", "queued", 1),
		job("j3", "w3", "author2", "claude", "queued", 1),
		job("j4", "w4", "author3", "gemini", "queued", 1),
		job("j5", "w5", "author4", "cursor", "queued", 1),
	}
	driver := testDriver(fake)

	if _, _, _, err := driver.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := fake.count("supervise.start"); got != 1 {
		t.Fatalf("supervise.start calls = %d, want 1 under max_active_jobs:1; calls=%#v", got, fake.calls)
	}
}

// C5 (RFC 0124): a paused run must hold — the driver does cleanup but launches
// no new lanes, stays non-terminal, announces once, and resumes on unpause.
func TestRunDriveHoldsPausedRun(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.pausedAt = "2026-06-14T00:00:00Z"
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	driver := testDriver(fake)

	actions, _, terminal, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if terminal {
		t.Fatalf("a paused run is not terminal")
	}
	if got := fake.count("session.register"); got != 0 {
		t.Fatalf("paused run must not register sessions, got %d", got)
	}
	if got := fake.count("supervise.start"); got != 0 {
		t.Fatalf("paused run must not supervise.start, got %d", got)
	}
	if actionIndex(actions, "paused") < 0 {
		t.Fatalf("expected a 'paused' hold action, got %#v", actions)
	}

	// Announce once: a second paused reconcile is quiet.
	actions2, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if actionIndex(actions2, "paused") >= 0 {
		t.Fatalf("paused notice must announce once, got a repeat: %#v", actions2)
	}

	// Resume: clearing paused_at lets the next reconcile launch the held slot.
	fake.pausedAt = ""
	if _, _, _, err := driver.ReconcileOnce(ctx); err != nil {
		t.Fatalf("resume reconcile: %v", err)
	}
	if got := fake.count("supervise.start"); got != 1 {
		t.Fatalf("after resume expected 1 supervise.start, got %d", got)
	}
}

func TestRunDriveAdoptsExistingSessions(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	fake.sessions = []map[string]any{session("sess_existing", "author", "codex", "active")}
	driver := testDriver(fake)

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 0 {
		t.Fatalf("session.register calls = %d, want 0", got)
	}
	if got := fake.count("supervise.start"); got != 0 {
		t.Fatalf("supervise.start calls = %d, want 0", got)
	}
	if len(actions) != 1 || actions[0].Action != "adopt" || actions[0].SessionID != "sess_existing" {
		t.Fatalf("actions = %#v, want adopt existing session", actions)
	}
}

// TestRunDriveSurfacesBlockedJobInsteadOfIdling (#389 gap 2): a run whose only
// non-terminal job is `blocked` with started_at set (a transient publish/seal
// failure left it stuck after the lane completed work) must produce a VISIBLE
// "cannot advance" action naming the seal-failure remediation — not an empty,
// silent reconcile. It must not launch or register anything.
func TestRunDriveSurfacesBlockedJobInsteadOfIdling(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	// started_at is set: the lane ran and finished but the seal failed (#389).
	sealFailedJob := job("job_design", "design_codex", "designer", "codex", "blocked", 1)
	sealFailedJob["started_at"] = "2026-01-01T00:00:00Z"
	fake.jobs = []map[string]any{sealFailedJob}
	driver := testDriver(fake)

	actions, _, terminal, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if terminal {
		t.Fatalf("a run with a blocked job is not terminal")
	}
	// Silent idle is the bug: there must be at least one action, and it must be
	// the loud cannot-advance signal.
	idx := actionIndex(actions, "cannot_advance")
	if idx < 0 {
		t.Fatalf("expected a 'cannot_advance' action for the blocked job, got %#v", actions)
	}
	signal := actions[idx]
	if signal.WorkflowJobID != "design_codex" || signal.Result != "cannot_advance_blocked" {
		t.Fatalf("cannot_advance action = %#v, want design_codex / cannot_advance_blocked", signal)
	}
	if !strings.Contains(signal.Message, "recovery reseal") {
		t.Fatalf("cannot_advance message = %q, want it to name `recovery reseal` (seal-failed path)", signal.Message)
	}
	if strings.Contains(signal.Message, "upstream dependency") {
		t.Fatalf("cannot_advance message = %q, must not claim dependency-blocked for a seal-failed job", signal.Message)
	}
	// The driver must NOT try to launch or register a blocked job.
	if got := fake.count("session.register"); got != 0 {
		t.Fatalf("session.register calls = %d, want 0 for a blocked job", got)
	}
	if got := fake.count("supervise.start"); got != 0 {
		t.Fatalf("supervise.start calls = %d, want 0 for a blocked job", got)
	}

	// The signal also reaches stderr (operator-visible), not just the action list.
	var stdout, stderr bytes.Buffer
	driver.options.Stdout = &stdout
	driver.options.Stderr = &stderr
	driver.emit(signal)
	if !strings.Contains(stderr.String(), "cannot advance design_codex") {
		t.Fatalf("emit did not write the cannot-advance warning to stderr: %q", stderr.String())
	}
}

// TestRunDriveSurfacesDependencyBlockedJobDistinctFromSealFailed (#446): a
// `blocked` job whose started_at is nil (never started — waiting on an upstream
// dependency) must NOT claim a seal failure in its cannot_advance message.
func TestRunDriveSurfacesDependencyBlockedJobDistinctFromSealFailed(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	// started_at is absent: the job is dependency-blocked, not seal-failed.
	fake.jobs = []map[string]any{job("job_review", "review_agy", "reviewer", "agy", "blocked", 1)}
	driver := testDriver(fake)

	actions, _, terminal, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if terminal {
		t.Fatalf("a run with a dependency-blocked job is not terminal")
	}
	idx := actionIndex(actions, "cannot_advance")
	if idx < 0 {
		t.Fatalf("expected a 'cannot_advance' action for the dependency-blocked job, got %#v", actions)
	}
	signal := actions[idx]
	if signal.WorkflowJobID != "review_agy" || signal.Result != "cannot_advance_blocked" {
		t.Fatalf("cannot_advance action = %#v, want review_agy / cannot_advance_blocked", signal)
	}
	// Dependency-blocked message must NOT say "lane finished" or "seal failed".
	if strings.Contains(signal.Message, "lane finished") {
		t.Fatalf("cannot_advance message = %q, must not claim the lane finished for a dependency-blocked job", signal.Message)
	}
	if strings.Contains(signal.Message, "seal failed") {
		t.Fatalf("cannot_advance message = %q, must not claim a seal failure for a dependency-blocked job", signal.Message)
	}
	// Must name the dependency-blocking context.
	if !strings.Contains(signal.Message, "upstream dependency") {
		t.Fatalf("cannot_advance message = %q, want it to describe upstream dependency blocking", signal.Message)
	}
}

func TestRunDriveClosesCompletedLanesBeforeFreshReviewer(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{
		job("job_author", "author_draft", "author", "codex", "completed", 1),
		job("job_review", "review", "reviewer", "agy", "queued", 1),
	}
	fake.sessions = []map[string]any{session("sess_author", "author", "codex", "active")}
	driver := testDriver(fake)
	driver.launched[slotKey{WorkflowJobID: "author_draft", Attempt: 1}] = "sess_author"

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stopIndex := fake.first("supervise.stop")
	registerIndex := fake.first("session.register")
	if stopIndex < 0 || registerIndex < 0 || stopIndex > registerIndex {
		t.Fatalf("call order = %#v, want supervise.stop before session.register; actions=%#v", fake.calls, actions)
	}
}

func TestRunDriveRefreshesSessionsAfterStoppingCompletedLane(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{
		job("job_author_1", "author_draft", "author", "codex", "completed", 1),
		job("job_author_2", "author_revision", "author", "codex", "queued", 1),
	}
	fake.sessions = []map[string]any{session("sess_author_1", "author", "codex", "active")}
	driver := testDriver(fake)
	driver.launched[slotKey{WorkflowJobID: "author_draft", Attempt: 1}] = "sess_author_1"

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 1 {
		t.Fatalf("session.register calls = %d, want 1; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	for _, action := range actions {
		if action.Action == "adopt" && action.SessionID == "sess_author_1" {
			t.Fatalf("stopped session was adopted by queued job: actions=%#v", actions)
		}
	}
}

func TestRunDriveRelaunchesWhenLaunchedSessionDies(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	driver := testDriver(fake)

	if _, _, _, err := driver.ReconcileOnce(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if got := fake.count("supervise.start"); got != 1 {
		t.Fatalf("supervise.start calls = %d, want 1; calls=%#v", got, fake.calls)
	}

	// The lane process died before claiming; the daemon no longer lists the
	// session, but the job is still queued in the same slot.
	fake.sessions = nil

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 2 {
		t.Fatalf("session.register calls = %d, want relaunch of the wedged slot; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	if got := fake.count("supervise.start"); got != 2 {
		t.Fatalf("supervise.start calls = %d, want 2; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	if got := fake.count("supervise.stop"); got != 0 {
		t.Fatalf("supervise.stop calls = %d, want 0 against an already-gone session; calls=%#v", got, fake.calls)
	}
	forgot := false
	for _, action := range actions {
		if action.Action == "forget" && action.SessionID == "sess_1" && action.WorkflowJobID == "author_draft" && action.Attempt == 1 {
			forgot = true
		}
	}
	if !forgot {
		t.Fatalf("actions = %#v, want a forget action for the dead launched session", actions)
	}
}

func TestRunDriveRelaunchesSameAttemptRequeue(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	driver := testDriver(fake)

	if _, _, _, err := driver.ReconcileOnce(ctx); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	// The lane claims the job, then stalls.
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "claimed", 1)}
	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("claimed reconcile: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("claimed reconcile should be quiet, got %#v", actions)
	}

	// Stale-lease recovery requeues the SAME (workflow_job_id, attempt) slot
	// and closes the stalled session.
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	fake.sessions[0]["state"] = "closed"

	actions, _, _, err = driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("requeue reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 2 {
		t.Fatalf("session.register calls = %d, want relaunch after same-attempt requeue; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	if got := fake.count("supervise.start"); got != 2 {
		t.Fatalf("supervise.start calls = %d, want 2; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	started := false
	for _, action := range actions {
		if action.Action == "supervise.start" && action.Result == "started" && action.SessionID == "sess_2" {
			started = true
		}
	}
	if !started {
		t.Fatalf("actions = %#v, want a fresh session started for the requeued slot", actions)
	}
}

func TestRunDriveStopsSupersededLaunchedAttempt(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_review", "review", "reviewer", "agy", "queued", 2)}
	fake.sessions = []map[string]any{session("sess_review_1", "reviewer", "agy", "active")}
	driver := testDriver(fake)
	driver.launched[slotKey{WorkflowJobID: "review", Attempt: 1}] = "sess_review_1"

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stopIndex := fake.first("supervise.stop")
	registerIndex := fake.first("session.register")
	if stopIndex < 0 || registerIndex < 0 || stopIndex > registerIndex {
		t.Fatalf("call order = %#v, want superseded attempt stopped before register; actions=%#v", fake.calls, actions)
	}
	if !fake.sessionState("sess_review_1", "closed") {
		t.Fatalf("superseded review session was not closed; sessions=%#v actions=%#v", fake.sessions, actions)
	}
}

func TestRunDriveStopsLaunchedLaneBeforeNeedsOperatorTerminal(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.runState = "needs_operator"
	fake.sessions = []map[string]any{session("sess_author", "author", "codex", "active")}
	driver := testDriver(fake)
	driver.launched[slotKey{WorkflowJobID: "author_draft", Attempt: 1}] = "sess_author"

	actions, state, terminal, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if state != "needs_operator" || !terminal {
		t.Fatalf("state/terminal = %q/%v, want needs_operator/true", state, terminal)
	}
	if got := fake.count("supervise.stop"); got != 1 {
		t.Fatalf("supervise.stop calls = %d, want 1; actions=%#v calls=%#v", got, actions, fake.calls)
	}
	if !fake.sessionState("sess_author", "closed") {
		t.Fatalf("launched session was not closed before terminal return; sessions=%#v actions=%#v", fake.sessions, actions)
	}
	stopIndex := actionIndex(actions, "supervise.stop")
	terminalIndex := actionIndex(actions, "terminal")
	if stopIndex < 0 || terminalIndex < 0 || stopIndex > terminalIndex {
		t.Fatalf("actions = %#v, want supervise.stop before terminal", actions)
	}
}

func TestRunDriveFreshRetryStopsOnlyCompletedRoleLane(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.freshRefusals = 1
	fake.jobs = []map[string]any{
		job("job_author_done", "author_draft", "author", "codex", "completed", 1),
		job("job_review", "review", "reviewer", "agy", "queued", 1),
	}
	fake.sessions = []map[string]any{
		session("sess_author_done", "author", "codex", "active"),
		session("sess_author_other_lane", "author", "agy", "active"),
	}
	driver := testDriver(fake)

	actions, _, _, err := driver.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := fake.count("session.register"); got != 2 {
		t.Fatalf("session.register calls = %d, want failed attempt plus retry; calls=%#v actions=%#v", got, fake.calls, actions)
	}
	if !fake.sessionState("sess_author_done", "closed") {
		t.Fatalf("completed author lane was not stopped; sessions=%#v actions=%#v", fake.sessions, actions)
	}
	if !fake.sessionState("sess_author_other_lane", "active") {
		t.Fatalf("other same-role lane was stopped; sessions=%#v actions=%#v", fake.sessions, actions)
	}
}

func TestRunDriveNeverCallsRescueVerbs(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	driver := testDriver(fake)

	if _, _, _, err := driver.ReconcileOnce(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	for _, call := range fake.calls {
		if !AllowedMethods[call.method] {
			t.Fatalf("method %s is outside run-drive allowlist", call.method)
		}
		switch call.method {
		case "recovery.requeue_stale", "review.override", "run.retry_job":
			t.Fatalf("run drive must not call rescue verb %s", call.method)
		}
		if force, _ := call.params["force"].(bool); force {
			t.Fatalf("run drive must not use --force-style params: %#v", call)
		}
	}
}

func TestRunDriveStopsFastOnProviderAuthRefusal(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
	fake.startErr = rpc.NewError("lane_provider_auth_failed", "raw provider output must not be replayed", nil)
	driver := New(fake, Options{
		RepositoryID:     "repo_1",
		RunID:            "run_1",
		ProviderAuthGate: "required",
		Now:              func() time.Time { return time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC) },
	})

	actions, _, _, err := driver.ReconcileOnce(ctx)
	var providerErr ProviderAuthRefusalError
	if !errors.As(err, &providerErr) || providerErr.Code != "lane_provider_auth_failed" {
		t.Fatalf("err = %#v, want ProviderAuthRefusalError lane_provider_auth_failed", err)
	}
	if got := fake.count("session.register"); got != 1 {
		t.Fatalf("session.register calls = %d, want 1; calls=%#v", got, fake.calls)
	}
	if got := fake.count("supervise.start"); got != 1 {
		t.Fatalf("supervise.start calls = %d, want 1; calls=%#v", got, fake.calls)
	}
	if got := fake.count("session.close"); got != 1 {
		t.Fatalf("session.close calls = %d, want 1; calls=%#v", got, fake.calls)
	}
	if len(actions) != 1 || actions[0].Action != "supervise.start" || actions[0].Result != "blocked" {
		t.Fatalf("actions = %#v, want one blocked supervise.start action", actions)
	}
	if strings.Contains(actions[0].Message, "raw provider output") {
		t.Fatalf("run-drive action replayed raw provider message: %#v", actions[0])
	}
	startCall := fake.calls[fake.first("supervise.start")]
	if startCall.params["provider_auth_gate"] != "required" {
		t.Fatalf("supervise.start params = %#v", startCall.params)
	}
	closeCall := fake.calls[fake.first("session.close")]
	if !strings.Contains(fmt.Sprint(closeCall.params["reason"]), "lane_provider_auth_failed") {
		t.Fatalf("session.close reason = %#v", closeCall.params["reason"])
	}
}

func TestRunDriveRunUsesDefaultSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := newFakeDrive()

	err := Run(ctx, fake, Options{
		RepositoryID: "repo_1",
		RunID:        "run_1",
		Interval:     time.Hour,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context.Canceled", err)
	}
}

func TestRunDriveWaitsOnWakeBetweenReconciles(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.wake = func(params map[string]any) (map[string]any, error) {
		if params["timeout_ms"] != int(time.Hour/time.Millisecond) {
			t.Fatalf("wake.wait params = %#v", params)
		}
		fake.runState = "completed"
		return map[string]any{
			"status": "notified",
			"event": map[string]any{
				"repository_id": "repo_1",
				"run_id":        "run_1",
				"kind":          "work_available",
			},
		}, nil
	}
	sleepCalled := false

	err := Run(ctx, fake, Options{
		RepositoryID: "repo_1",
		RunID:        "run_1",
		Interval:     time.Hour,
		Sleep: func(context.Context, time.Duration) error {
			sleepCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if got := fake.count("wake.wait"); got != 1 {
		t.Fatalf("wake.wait calls = %d, want 1; calls=%#v", got, fake.calls)
	}
	if sleepCalled {
		t.Fatal("run drive used fixed sleep despite wake.wait being available")
	}
}

func TestRunDriveWakeTimeoutKeepsBoundedFallback(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.completeOnStart = true
	fake.wake = func(params map[string]any) (map[string]any, error) {
		if fake.count("wake.wait") == 1 {
			fake.jobs = []map[string]any{job("job_author", "author_draft", "author", "codex", "queued", 1)}
		}
		return map[string]any{"status": "timeout"}, nil
	}
	sleepCalled := false

	err := Run(ctx, fake, Options{
		RepositoryID: "repo_1",
		RunID:        "run_1",
		Interval:     time.Hour,
		Sleep: func(context.Context, time.Duration) error {
			sleepCalled = true
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if got := fake.count("supervise.start"); got != 1 {
		t.Fatalf("supervise.start calls = %d, want 1; calls=%#v", got, fake.calls)
	}
	if got := fake.count("wake.wait"); got < 1 {
		t.Fatalf("wake.wait calls = %d, want at least 1; calls=%#v", got, fake.calls)
	}
	if sleepCalled {
		t.Fatal("run drive used fixed sleep instead of wake timeout fallback")
	}
}

type fakeDrive struct {
	runState        string
	pausedAt        string
	jobs            []map[string]any
	sessions        []map[string]any
	workflow        map[string]any
	calls           []fakeCall
	nextID          int
	freshRefusals   int
	startErr        error
	completeOnStart bool
	wake            func(map[string]any) (map[string]any, error)
}

type fakeCall struct {
	method string
	params map[string]any
}

func newFakeDrive() *fakeDrive {
	return &fakeDrive{runState: "running", nextID: 1}
}

func (f *fakeDrive) Invoke(_ context.Context, method string, params map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, fakeCall{method: method, params: cloneParams(params)})
	switch method {
	case "run.detail":
		run := map[string]any{"run_id": params["run_id"], "state": f.runState}
		if f.pausedAt != "" {
			run["paused_at"] = f.pausedAt
		}
		workflow := f.workflow
		if workflow == nil {
			workflow = map[string]any{"jobs": []any{}}
		}
		return map[string]any{
			"run":      run,
			"jobs":     cloneRows(f.jobs),
			"workflow": workflow,
		}, nil
	case "list.sessions":
		return map[string]any{"items": cloneRows(f.sessions)}, nil
	case "session.register":
		if f.freshRefusals > 0 {
			f.freshRefusals--
			return nil, errors.New("reviewer_context_policy: fresh registration refused; pass --force-non-fresh to override")
		}
		role := fmt.Sprint(params["role"])
		lane := fmt.Sprint(params["lane"])
		sessionID := fmt.Sprintf("sess_%d", f.nextID)
		f.nextID++
		f.sessions = append(f.sessions, session(sessionID, role, lane, "active"))
		return map[string]any{"session_id": sessionID}, nil
	case "supervise.start":
		if f.startErr != nil {
			return nil, f.startErr
		}
		if f.completeOnStart {
			f.runState = "completed"
		}
		return map[string]any{"state": "attached", "session_id": params["session_id"]}, nil
	case "wake.wait":
		if f.wake != nil {
			return f.wake(params)
		}
		return nil, errors.New("unexpected method: " + method)
	case "supervise.stop":
		sessionID := fmt.Sprint(params["session_id"])
		for _, sess := range f.sessions {
			if fmt.Sprint(sess["session_id"]) == sessionID {
				sess["state"] = "closed"
			}
		}
		return map[string]any{"state": "stopped", "session_id": sessionID}, nil
	case "session.close":
		sessionID := fmt.Sprint(params["session_id"])
		for _, sess := range f.sessions {
			if fmt.Sprint(sess["session_id"]) == sessionID {
				sess["state"] = "closed"
			}
		}
		return map[string]any{"state": "closed", "session_id": params["session_id"]}, nil
	default:
		return nil, errors.New("unexpected method: " + method)
	}
}

func (f *fakeDrive) count(method string) int {
	total := 0
	for _, call := range f.calls {
		if call.method == method {
			total++
		}
	}
	return total
}

func (f *fakeDrive) first(method string) int {
	for i, call := range f.calls {
		if call.method == method {
			return i
		}
	}
	return -1
}

func (f *fakeDrive) sessionState(sessionID string, state string) bool {
	for _, sess := range f.sessions {
		if fmt.Sprint(sess["session_id"]) == sessionID {
			return fmt.Sprint(sess["state"]) == state
		}
	}
	return false
}

func actionIndex(actions []Action, action string) int {
	for i, item := range actions {
		if item.Action == action {
			return i
		}
	}
	return -1
}

func testDriver(fake *fakeDrive) *Driver {
	return New(fake, Options{
		RepositoryID: "repo_1",
		RunID:        "run_1",
		Once:         true,
		Now: func() time.Time {
			return time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
		},
	})
}

func job(jobID, workflowJobID, role, lane, state string, attempt int) map[string]any {
	return map[string]any{
		"job_id":             jobID,
		"workflow_job_id":    workflowJobID,
		"role_id":            role,
		"state":              state,
		"attempt":            attempt,
		"lane_selector_json": map[string]any{"lane_id": lane},
	}
}

func session(sessionID, role, lane, state string) map[string]any {
	return map[string]any{
		"session_id": sessionID,
		"role_id":    role,
		"lane_id":    lane,
		"state":      state,
	}
}

func cloneRows(rows []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, cloneParams(row))
	}
	return result
}

func cloneParams(row map[string]any) map[string]any {
	result := make(map[string]any, len(row))
	for key, value := range row {
		result[key] = value
	}
	return result
}

// markerDriver builds a Driver with just enough Options for claimAdvisoryMarker.
func markerDriver(repoRoot string, pid int, force bool, stderr *bytes.Buffer) *Driver {
	return New(nil, Options{
		RepositoryID:    "repo_1",
		RunID:           "run_marker",
		RepoRoot:        repoRoot,
		PID:             pid,
		ForceConcurrent: force,
		Stderr:          stderr,
	})
}

func markerPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, ".striatum", "scratch", "run-drive-run_marker.pid")
}

// deadPID returns a pid that is reliably not alive: it forks a trivial child,
// waits for it to exit, and returns its (now reaped) pid.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn helper for dead pid: %v", err)
	}
	return cmd.ProcessState.Pid()
}

// #293 claim 2: a live pre-existing drive for the same run must REFUSE the
// second drive (not silently coexist behind the daemon double-claim guard).
func TestClaimAdvisoryMarkerRefusesLiveDuplicate(t *testing.T) {
	repoRoot := t.TempDir()
	// First driver (the "existing" live drive: use our own pid, which is alive).
	livePID := os.Getpid()
	first := markerDriver(repoRoot, livePID, false, &bytes.Buffer{})
	cleanup1, err := first.claimAdvisoryMarker()
	if err != nil {
		t.Fatalf("first claim should succeed: %v", err)
	}
	defer cleanup1()

	// Second driver, different pid, marker names the live first pid -> refuse.
	var stderr bytes.Buffer
	second := markerDriver(repoRoot, livePID+100000, false, &stderr)
	_, err = second.claimAdvisoryMarker()
	var concurrent ConcurrentDriveError
	if !errors.As(err, &concurrent) {
		t.Fatalf("expected ConcurrentDriveError, got %v", err)
	}
	if concurrent.PID != livePID {
		t.Errorf("ConcurrentDriveError.PID = %d, want %d", concurrent.PID, livePID)
	}
	// The first drive's marker must be intact (the refusal must not clobber it).
	data, readErr := os.ReadFile(markerPath(t, repoRoot))
	if readErr != nil {
		t.Fatalf("marker should still exist after refusal: %v", readErr)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(livePID) {
		t.Errorf("marker = %q, want live pid %d preserved", string(data), livePID)
	}
}

// #293 claim 2: a stale marker (dead pid) must be reaped, not block the drive.
func TestClaimAdvisoryMarkerReapsDeadMarker(t *testing.T) {
	repoRoot := t.TempDir()
	dir := filepath.Join(repoRoot, ".striatum", "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dead := deadPID(t)
	if err := os.WriteFile(markerPath(t, repoRoot), []byte(strconv.Itoa(dead)+"\n"), 0o600); err != nil {
		t.Fatalf("seed stale marker: %v", err)
	}

	mine := os.Getpid()
	d := markerDriver(repoRoot, mine, false, &bytes.Buffer{})
	cleanup, err := d.claimAdvisoryMarker()
	if err != nil {
		t.Fatalf("stale (dead-pid) marker must not refuse: %v", err)
	}
	defer cleanup()
	data, readErr := os.ReadFile(markerPath(t, repoRoot))
	if readErr != nil {
		t.Fatalf("marker should exist after reap: %v", readErr)
	}
	if strings.TrimSpace(string(data)) != strconv.Itoa(mine) {
		t.Errorf("stale marker not reaped: marker = %q, want our pid %d", string(data), mine)
	}
}

// #293 claim 2: --force-concurrent (Options.ForceConcurrent) opts into
// co-driving for the documented background + foreground-waiter composition.
func TestClaimAdvisoryMarkerForceConcurrentCoDrives(t *testing.T) {
	repoRoot := t.TempDir()
	livePID := os.Getpid()
	first := markerDriver(repoRoot, livePID, false, &bytes.Buffer{})
	cleanup1, err := first.claimAdvisoryMarker()
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	defer cleanup1()

	var stderr bytes.Buffer
	second := markerDriver(repoRoot, livePID+100000, true, &stderr)
	cleanup2, err := second.claimAdvisoryMarker()
	if err != nil {
		t.Fatalf("--force-concurrent must allow co-drive, got %v", err)
	}
	defer cleanup2()
	if !strings.Contains(stderr.String(), "co-driving") {
		t.Errorf("force-concurrent should warn about co-driving, got %q", stderr.String())
	}
}

// --- #513: reconnect-with-backoff on transient daemon_unreachable ---

// flakyInvoker fails its first failFor Invoke calls with err, then delegates to
// the wrapped invoker. It lets a reconnect test simulate "the daemon socket
// disappeared during a restart and returned within seconds".
type flakyInvoker struct {
	inner   Invoker
	err     error
	failFor int
	calls   int
}

func (f *flakyInvoker) Invoke(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	f.calls++
	if f.calls <= f.failFor {
		return nil, f.err
	}
	return f.inner.Invoke(ctx, method, params)
}

// reconnectDriver builds a Driver with an advancing clock and a counting,
// instant Sleep so the reconnect-budget deadline can actually expire in a unit
// test. step is how far the clock advances per Sleep.
func reconnectDriver(inv Invoker, step time.Duration, sleeps *int, budget time.Duration) *Driver {
	now := time.Date(2026, 6, 20, 14, 50, 0, 0, time.UTC)
	d := New(inv, Options{
		RepositoryID:    "repo_1",
		RunID:           "run_1",
		Once:            true,
		ReconnectBudget: budget,
		ReconnectStep:   time.Millisecond,
		Now:             func() time.Time { return now },
		Sleep: func(_ context.Context, _ time.Duration) error {
			*sleeps++
			now = now.Add(step)
			return nil
		},
		Stderr: &bytes.Buffer{},
	})
	return d
}

// #513: a transient daemon_unreachable (the socket vanished during a daemon
// restart and returns within seconds) must be retried with backoff, NOT exit
// the driver with status=11 abandoning a live, resumable run.
func TestRunDriveReconnectsTransientDaemonUnreachable(t *testing.T) {
	ctx := context.Background()
	fake := newFakeDrive()
	fake.jobs = []map[string]any{job("j1", "w1", "author", "codex", "queued", 1)}
	sleeps := 0
	// Clock advances 100ms per reconnect attempt; budget 30s; fail twice then heal.
	flaky := &flakyInvoker{
		inner:   fake,
		err:     &rpcclient.Error{Code: "daemon_unreachable", Message: "daemon unreachable at /run/striatum/rpc/daemon-go.sock: dial unix: connect: no such file or directory", ExitCode: 11},
		failFor: 2,
	}
	d := reconnectDriver(flaky, 100*time.Millisecond, &sleeps, 30*time.Second)
	actions, _, _, err := d.ReconcileOnce(ctx)
	if err != nil {
		t.Fatalf("reconcile should recover after transient unreachable, got %v", err)
	}
	if sleeps == 0 {
		t.Fatalf("expected at least one reconnect backoff sleep, got 0")
	}
	if fake.count("session.register") != 1 {
		t.Fatalf("driver should have resumed and registered the queued lane after reconnect; actions=%#v calls=%#v", actions, fake.calls)
	}
}

// #513: if the daemon never comes back within the reconnect budget, the invoke
// must give up and surface the unreachable error (so a genuinely dead daemon
// still fails loudly / lets systemd Restart=on-failure take over) rather than
// looping forever.
func TestRunDriveReconnectGivesUpAfterBudget(t *testing.T) {
	ctx := context.Background()
	unreach := &rpcclient.Error{Code: "daemon_unreachable", Message: "daemon unreachable: no such file or directory", ExitCode: 11}
	sleeps := 0
	// Always fail; clock advances 5s per attempt; budget 30s => bounded loop.
	flaky := &flakyInvoker{inner: newFakeDrive(), err: unreach, failFor: 1 << 30}
	d := reconnectDriver(flaky, 5*time.Second, &sleeps, 30*time.Second)
	_, _, _, err := d.ReconcileOnce(ctx)
	if err == nil {
		t.Fatalf("expected unreachable error after budget exhausts, got nil")
	}
	if !isTransientUnreachable(err) {
		t.Fatalf("expected the surfaced error to be the unreachable error, got %v", err)
	}
	if sleeps == 0 {
		t.Fatalf("expected the driver to have attempted reconnect before giving up")
	}
	// Bounded: 30s budget / 5s per attempt is well under 100 attempts.
	if flaky.calls > 100 {
		t.Fatalf("reconnect loop is not bounded: %d invoke attempts", flaky.calls)
	}
}

// #513: a non-transient error (e.g. an ordinary daemon-side rejection) must NOT
// be retried — only transient unreachability is recoverable.
func TestRunDriveDoesNotRetryNonTransientError(t *testing.T) {
	ctx := context.Background()
	sleeps := 0
	flaky := &flakyInvoker{inner: newFakeDrive(), err: errors.New("invalid_transition: nope"), failFor: 1 << 30}
	d := reconnectDriver(flaky, time.Second, &sleeps, 30*time.Second)
	_, _, _, err := d.ReconcileOnce(ctx)
	if err == nil {
		t.Fatalf("expected the non-transient error to propagate")
	}
	if sleeps != 0 {
		t.Fatalf("a non-transient error must not trigger reconnect backoff, got %d sleeps", sleeps)
	}
	if flaky.calls != 1 {
		t.Fatalf("a non-transient error must fail on the first attempt, got %d", flaky.calls)
	}
}

func TestIsTransientUnreachable(t *testing.T) {
	transient := []error{
		&rpcclient.Error{Code: "daemon_unreachable", Message: "x"},
		&rpc.Error{Code: "daemon_unreachable", Message: "x"},
		errors.New("daemon unreachable at /run/striatum/rpc/daemon-go.sock: dial unix: connect: no such file or directory"),
		errors.New("connect: connection refused"),
	}
	for _, err := range transient {
		if !isTransientUnreachable(err) {
			t.Errorf("isTransientUnreachable(%v) = false, want true", err)
		}
	}
	notTransient := []error{
		nil,
		errors.New("invalid_transition"),
		&rpcclient.Error{Code: "capability_missing", Message: "x"},
		context.Canceled,
		context.DeadlineExceeded,
	}
	for _, err := range notTransient {
		if isTransientUnreachable(err) {
			t.Errorf("isTransientUnreachable(%v) = true, want false", err)
		}
	}
}
