package runreconcile

import (
	"reflect"
	"strings"
	"testing"
)

func job(workflowJobID, role, lane, state string, attempt int) map[string]any {
	m := map[string]any{
		"workflow_job_id": workflowJobID,
		"role_id":         role,
		"state":           state,
		"attempt":         attempt,
	}
	if lane != "" {
		m["lane_selector_json"] = map[string]any{"lane_id": lane}
	}
	return m
}

func session(sessionID, role, lane, state string) map[string]any {
	return map[string]any{
		"session_id": sessionID,
		"role_id":    role,
		"lane_id":    lane,
		"state":      state,
	}
}

// kinds projects a decision slice down to (slot, kind, session) tuples so the
// parity assertions read clearly.
type decisionShape struct {
	WorkflowJobID string
	Attempt       int
	Kind          LaunchKind
	Role          string
	Lane          string
	SessionID     string
}

func shapes(decisions []LaunchDecision) []decisionShape {
	out := make([]decisionShape, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, decisionShape{
			WorkflowJobID: d.Slot.WorkflowJobID,
			Attempt:       d.Slot.Attempt,
			Kind:          d.Kind,
			Role:          d.Role,
			Lane:          d.Lane,
			SessionID:     d.SessionID,
		})
	}
	return out
}

// TestReconcilePredicateParityRegistersFreshForQueued: queued jobs with no
// adoptable sessions each become a LaunchRegisterFresh decision — the spawn the
// driver and the daemon scheduler must make identically (C3).
func TestReconcilePredicateParityRegistersFreshForQueued(t *testing.T) {
	jobs := []map[string]any{
		job("author_draft", "author", "codex", "queued", 1),
		job("review", "reviewer", "agy", "queued", 1),
	}
	got := shapes(PlanLaunch(jobs, nil, nil, nil))
	want := []decisionShape{
		{WorkflowJobID: "author_draft", Attempt: 1, Kind: LaunchRegisterFresh, Role: "author", Lane: "codex"},
		{WorkflowJobID: "review", Attempt: 1, Kind: LaunchRegisterFresh, Role: "reviewer", Lane: "agy"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParityAdoptsExistingSession: an unused active session for
// the role+lane slot is adopted rather than re-registered.
func TestReconcilePredicateParityAdoptsExistingSession(t *testing.T) {
	jobs := []map[string]any{job("author_draft", "author", "codex", "queued", 1)}
	sessions := []map[string]any{session("sess_existing", "author", "codex", "active")}
	got := shapes(PlanLaunch(jobs, nil, sessions, nil))
	want := []decisionShape{
		{WorkflowJobID: "author_draft", Attempt: 1, Kind: LaunchAdoptExisting, Role: "author", Lane: "codex", SessionID: "sess_existing"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParitySkipsAlreadyLaunchedSlot: a slot already launched
// (present in the launched map) yields no decision — the idempotent re-derive
// both homes rely on for restart safety (C4).
func TestReconcilePredicateParitySkipsAlreadyLaunchedSlot(t *testing.T) {
	jobs := []map[string]any{job("author_draft", "author", "codex", "queued", 1)}
	launched := map[SlotKey]string{{WorkflowJobID: "author_draft", Attempt: 1}: "sess_1"}
	if got := PlanLaunch(jobs, nil, nil, launched); len(got) != 0 {
		t.Fatalf("decisions = %#v, want none for an already-launched slot", got)
	}
}

// TestReconcilePredicateParityAmbiguousLane: a queued job whose lane cannot be
// resolved is flagged ambiguous, never auto-spawned.
func TestReconcilePredicateParityAmbiguousLane(t *testing.T) {
	jobs := []map[string]any{job("mystery", "author", "", "queued", 1)}
	got := shapes(PlanLaunch(jobs, map[string]any{"jobs": []any{}}, nil, nil))
	want := []decisionShape{
		{WorkflowJobID: "mystery", Attempt: 1, Kind: LaunchAmbiguousLane, Role: "author"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParitySharedRoleLaneAdoptsOnceThenHoldsLane: two queued
// jobs sharing a role+lane with a single active session — the first adopts it,
// and the second is HELD (no decision) because the per-lane cap of 1 (#322)
// forbids two jobs on one lane concurrently. The held job becomes eligible again
// on a later pass once the lane frees, which keeps the #290/#302 same-lane wedge
// from re-opening. This also still pins the in-pass used-session bookkeeping.
func TestReconcilePredicateParitySharedRoleLaneAdoptsOnceThenHoldsLane(t *testing.T) {
	jobs := []map[string]any{
		job("a1", "author", "codex", "queued", 1),
		job("a2", "author", "codex", "queued", 1),
	}
	sessions := []map[string]any{session("sess_one", "author", "codex", "active")}
	got := shapes(PlanLaunch(jobs, nil, sessions, nil))
	want := []decisionShape{
		{WorkflowJobID: "a1", Attempt: 1, Kind: LaunchAdoptExisting, Role: "author", Lane: "codex", SessionID: "sess_one"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParityLaunchedSessionExcludedFromAdoption: a session that
// already serves a launched slot is never adopted by another queued job.
func TestReconcilePredicateParityLaunchedSessionExcludedFromAdoption(t *testing.T) {
	jobs := []map[string]any{job("a2", "author", "codex", "queued", 1)}
	sessions := []map[string]any{session("sess_used", "author", "codex", "active")}
	launched := map[SlotKey]string{{WorkflowJobID: "a1", Attempt: 1}: "sess_used"}
	got := shapes(PlanLaunch(jobs, nil, sessions, launched))
	want := []decisionShape{
		{WorkflowJobID: "a2", Attempt: 1, Kind: LaunchRegisterFresh, Role: "author", Lane: "codex"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParityResolvesLaneFromWorkflowSnapshot: when the job row
// carries no lane, the lane is resolved from the workflow snapshot's jobs list.
func TestReconcilePredicateParityResolvesLaneFromWorkflowSnapshot(t *testing.T) {
	jobs := []map[string]any{job("build", "author", "", "queued", 1)}
	workflow := map[string]any{"jobs": []any{
		map[string]any{"id": "build", "lane_id": "claude"},
	}}
	got := shapes(PlanLaunch(jobs, workflow, nil, nil))
	want := []decisionShape{
		{WorkflowJobID: "build", Attempt: 1, Kind: LaunchRegisterFresh, Role: "author", Lane: "claude"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestReconcilePredicateParityOnlyQueuedJobsDecide: non-queued jobs are inert.
func TestReconcilePredicateParityOnlyQueuedJobsDecide(t *testing.T) {
	jobs := []map[string]any{
		job("done", "author", "codex", "completed", 1),
		job("running", "author", "codex", "claimed", 1),
		job("next", "reviewer", "agy", "queued", 1),
	}
	got := shapes(PlanLaunch(jobs, nil, nil, nil))
	want := []decisionShape{
		{WorkflowJobID: "next", Attempt: 1, Kind: LaunchRegisterFresh, Role: "reviewer", Lane: "agy"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decisions = %#v, want %#v", got, want)
	}
}

// TestPlanLaunchSurfacesBlockedJobAsUnadvanceable (#389 gap 2): a `blocked` job
// is non-terminal but not queued and not in-flight, so the driver would
// otherwise emit nothing and idle silently. PlanLaunch must surface it as a
// LaunchUnadvanceable decision carrying the state and a remediation hint — a
// visible "cannot advance" signal, never a silent skip.
//
// This sub-test covers the seal-failed variant (started_at set): the
// remediation must name `recovery reseal`. A separate test (#446) covers the
// dependency-blocked variant (no started_at).
func TestPlanLaunchSurfacesBlockedJobAsUnadvanceable(t *testing.T) {
	// started_at set → seal-failed path (#389): lane ran but seal failed.
	sealFailedJob := job("design_codex", "designer", "codex", "blocked", 1)
	sealFailedJob["started_at"] = "2026-01-01T00:00:00Z"
	jobs := []map[string]any{sealFailedJob}
	got := PlanLaunch(jobs, nil, nil, nil)
	if len(got) != 1 {
		t.Fatalf("decisions = %#v, want exactly one unadvanceable decision", shapes(got))
	}
	d := got[0]
	if d.Kind != LaunchUnadvanceable {
		t.Fatalf("kind = %v, want LaunchUnadvanceable; decision=%#v", d.Kind, d)
	}
	if d.State != "blocked" {
		t.Fatalf("state = %q, want blocked", d.State)
	}
	if d.Slot.WorkflowJobID != "design_codex" || d.Role != "designer" {
		t.Fatalf("slot/role = %#v / %q, want design_codex / designer", d.Slot, d.Role)
	}
	if !strings.Contains(d.Remediation, "recovery reseal") {
		t.Fatalf("remediation = %q, want it to name `recovery reseal` (seal-failed path)", d.Remediation)
	}
	if strings.Contains(d.Remediation, "upstream dependency") {
		t.Fatalf("remediation = %q, must not claim dependency-blocked for a seal-failed job", d.Remediation)
	}
}

// TestPlanLaunchDependencyBlockedDistinctFromSealFailed (#446): a `blocked` job
// whose started_at is nil (never ran — waiting on upstream) must surface an
// accurate dependency-blocked message, NOT a seal-failure claim.
func TestPlanLaunchDependencyBlockedDistinctFromSealFailed(t *testing.T) {
	// no started_at → dependency-blocked path (#446): upstream not yet done.
	depBlockedJob := job("review_codex", "reviewer", "agy", "blocked", 1)
	// started_at deliberately absent (nil in the map)
	jobs := []map[string]any{depBlockedJob}
	got := PlanLaunch(jobs, nil, nil, nil)
	if len(got) != 1 {
		t.Fatalf("decisions = %#v, want exactly one unadvanceable decision", shapes(got))
	}
	d := got[0]
	if d.Kind != LaunchUnadvanceable {
		t.Fatalf("kind = %v, want LaunchUnadvanceable; decision=%#v", d.Kind, d)
	}
	if d.State != "blocked" {
		t.Fatalf("state = %q, want blocked", d.State)
	}
	// Must NOT claim the lane finished or a seal failed.
	if strings.Contains(d.Remediation, "lane finished") {
		t.Fatalf("remediation = %q, must not claim lane finished for dependency-blocked job", d.Remediation)
	}
	if strings.Contains(d.Remediation, "seal failed") {
		t.Fatalf("remediation = %q, must not claim seal failure for dependency-blocked job", d.Remediation)
	}
	if !strings.Contains(d.Remediation, "upstream dependency") {
		t.Fatalf("remediation = %q, want it to describe upstream dependency blocking", d.Remediation)
	}
}

// TestUnadvanceableRemediationBothBlockedPaths (#446): unit-tests the
// UnadvanceableRemediation helper for both the seal-failed and
// dependency-blocked variants of a `blocked` job.
func TestUnadvanceableRemediationBothBlockedPaths(t *testing.T) {
	t.Run("seal_failed", func(t *testing.T) {
		j := map[string]any{"state": "blocked", "started_at": "2026-01-01T00:00:00Z"}
		msg := UnadvanceableRemediation(j)
		if !strings.Contains(msg, "recovery reseal") {
			t.Fatalf("seal-failed remediation = %q, want it to name `recovery reseal`", msg)
		}
		if strings.Contains(msg, "upstream dependency") {
			t.Fatalf("seal-failed remediation = %q, must not mention upstream dependency", msg)
		}
	})
	t.Run("dependency_blocked", func(t *testing.T) {
		j := map[string]any{"state": "blocked"} // no started_at
		msg := UnadvanceableRemediation(j)
		if strings.Contains(msg, "lane finished") {
			t.Fatalf("dep-blocked remediation = %q, must not claim lane finished", msg)
		}
		if strings.Contains(msg, "seal failed") {
			t.Fatalf("dep-blocked remediation = %q, must not claim seal failed", msg)
		}
		if !strings.Contains(msg, "upstream dependency") {
			t.Fatalf("dep-blocked remediation = %q, want it to describe upstream dependency", msg)
		}
	})
	t.Run("dependency_blocked_explicit_nil", func(t *testing.T) {
		j := map[string]any{"state": "blocked", "started_at": nil}
		msg := UnadvanceableRemediation(j)
		if strings.Contains(msg, "lane finished") || strings.Contains(msg, "seal failed") {
			t.Fatalf("dep-blocked (nil started_at) remediation = %q, must not claim seal failure", msg)
		}
		if !strings.Contains(msg, "upstream dependency") {
			t.Fatalf("dep-blocked (nil started_at) remediation = %q, want upstream dependency", msg)
		}
	})
}

// TestPlanLaunchBlockedAndQueuedTogether: a run with one blocked job and one
// queued job emits BOTH the unadvanceable signal and the launch — the stuck job
// never masks the launchable one and vice versa.
func TestPlanLaunchBlockedAndQueuedTogether(t *testing.T) {
	jobs := []map[string]any{
		job("blocked_job", "author", "codex", "blocked", 1),
		job("ready_job", "reviewer", "agy", "queued", 1),
	}
	got := PlanLaunch(jobs, nil, nil, nil)
	var sawUnadvanceable, sawLaunch bool
	for _, d := range got {
		switch d.Kind {
		case LaunchUnadvanceable:
			sawUnadvanceable = d.Slot.WorkflowJobID == "blocked_job"
		case LaunchRegisterFresh:
			sawLaunch = d.Slot.WorkflowJobID == "ready_job"
		}
	}
	if !sawUnadvanceable || !sawLaunch {
		t.Fatalf("decisions = %#v, want both the blocked-job signal and the queued-job launch", shapes(got))
	}
}

// TestPlanLaunchNoUnadvanceableForTerminalOrInFlight: terminal and in-flight
// states are never surfaced as unadvanceable — only `blocked` is. This guards
// against false "cannot advance" noise on healthy completed / running jobs.
func TestPlanLaunchNoUnadvanceableForTerminalOrInFlight(t *testing.T) {
	for _, state := range []string{"completed", "failed", "canceled", "skipped", "claimed", "running", "stale_lease", "waiting_human"} {
		jobs := []map[string]any{job("j", "author", "codex", state, 1)}
		for _, d := range PlanLaunch(jobs, nil, nil, nil) {
			if d.Kind == LaunchUnadvanceable {
				t.Fatalf("state %q produced an unadvanceable decision; should be inert: %#v", state, d)
			}
		}
		if UnadvanceableJobState(state) {
			t.Fatalf("UnadvanceableJobState(%q) = true, want false", state)
		}
	}
	if !UnadvanceableJobState("blocked") {
		t.Fatal("UnadvanceableJobState(blocked) = false, want true")
	}
}

// countLaunches counts the real launch decisions (adopt + register fresh),
// excluding AmbiguousLane skips, which never consume cap budget.
func countLaunches(decisions []LaunchDecision) int {
	n := 0
	for _, d := range decisions {
		if d.Kind == LaunchAdoptExisting || d.Kind == LaunchRegisterFresh {
			n++
		}
	}
	return n
}

// TestPlanLaunchHonorsMaxActiveJobs: with max_active_jobs:1 and several queued
// jobs on distinct lanes (none in-flight), exactly ONE launch decision is emitted.
func TestPlanLaunchHonorsMaxActiveJobs(t *testing.T) {
	jobs := []map[string]any{
		job("a", "author", "codex", "queued", 1),
		job("b", "reviewer", "agy", "queued", 1),
		job("c", "author2", "claude", "queued", 1),
	}
	workflow := map[string]any{"parallelism": map[string]any{"max_active_jobs": 1}}
	got := PlanLaunch(jobs, workflow, nil, nil)
	if n := countLaunches(got); n != 1 {
		t.Fatalf("launches = %d, want 1; decisions=%#v", n, shapes(got))
	}
}

// TestPlanLaunchCountsRunningTowardCap: a running (or claimed) job already
// occupies the only active-lane slot, so a cap of 1 emits ZERO launches.
func TestPlanLaunchCountsRunningTowardCap(t *testing.T) {
	for _, inFlight := range []string{"running", "claimed", "stale_lease"} {
		jobs := []map[string]any{
			job("hot", "author", "codex", inFlight, 1),
			job("q1", "reviewer", "agy", "queued", 1),
			job("q2", "author2", "claude", "queued", 1),
		}
		workflow := map[string]any{"parallelism": map[string]any{"max_active_jobs": 1}}
		got := PlanLaunch(jobs, workflow, nil, nil)
		if n := countLaunches(got); n != 0 {
			t.Fatalf("in-flight=%s: launches = %d, want 0; decisions=%#v", inFlight, n, shapes(got))
		}
	}
}

// TestPlanLaunchPerLaneCapOfOne: cap 2 leaves global budget for both, but two
// queued jobs on the SAME role+lane may only put ONE job on that lane at a time.
func TestPlanLaunchPerLaneCapOfOne(t *testing.T) {
	jobs := []map[string]any{
		job("a1", "author", "codex", "queued", 1),
		job("a2", "author", "codex", "queued", 1),
	}
	workflow := map[string]any{"parallelism": map[string]any{"max_active_jobs": 2}}
	got := PlanLaunch(jobs, workflow, nil, nil)
	launches := 0
	for _, d := range got {
		if (d.Kind == LaunchAdoptExisting || d.Kind == LaunchRegisterFresh) && d.Lane == "codex" {
			launches++
		}
	}
	if launches > 1 {
		t.Fatalf("codex-lane launches = %d, want at most 1; decisions=%#v", launches, shapes(got))
	}
}

// TestPlanLaunchUnlimitedWhenCapAbsentOrZero: no parallelism, or
// max_active_jobs:0, means unlimited — every distinct-lane queued job launches
// (backward-compat for the existing field-less workflows).
func TestPlanLaunchUnlimitedWhenCapAbsentOrZero(t *testing.T) {
	jobs := []map[string]any{
		job("a", "author", "codex", "queued", 1),
		job("b", "reviewer", "agy", "queued", 1),
		job("c", "author2", "claude", "queued", 1),
	}
	for name, workflow := range map[string]map[string]any{
		"nil-workflow":      nil,
		"no-parallelism":    {"jobs": []any{}},
		"max_active_jobs:0": {"parallelism": map[string]any{"max_active_jobs": 0}},
	} {
		got := PlanLaunch(jobs, workflow, nil, nil)
		if n := countLaunches(got); n != 3 {
			t.Fatalf("%s: launches = %d, want 3; decisions=%#v", name, n, shapes(got))
		}
	}
}

func TestMaxActiveJobs(t *testing.T) {
	cases := []struct {
		name     string
		workflow map[string]any
		want     int
	}{
		{"nil", nil, 0},
		{"absent", map[string]any{"jobs": []any{}}, 0},
		{"zero", map[string]any{"parallelism": map[string]any{"max_active_jobs": 0}}, 0},
		{"negative", map[string]any{"parallelism": map[string]any{"max_active_jobs": -3}}, 0},
		{"positive", map[string]any{"parallelism": map[string]any{"max_active_jobs": 4}}, 4},
		{"string", map[string]any{"parallelism": map[string]any{"max_active_jobs": "2"}}, 2},
		{"float", map[string]any{"parallelism": map[string]any{"max_active_jobs": float64(3)}}, 3},
	}
	for _, tc := range cases {
		if got := MaxActiveJobs(tc.workflow); got != tc.want {
			t.Fatalf("%s: MaxActiveJobs = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSortedJobsOrdersByWorkflowJobIDThenAttempt(t *testing.T) {
	jobs := []map[string]any{
		job("b", "r", "l", "queued", 1),
		job("a", "r", "l", "queued", 2),
		job("a", "r", "l", "queued", 1),
	}
	sorted := SortedJobs(jobs)
	var order []string
	for _, j := range sorted {
		order = append(order, StringValue(j["workflow_job_id"])+":"+StringValue(j["attempt"]))
	}
	want := []string{"a:1", "a:2", "b:1"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %#v, want %#v", order, want)
	}
}

func TestIsTerminalRunState(t *testing.T) {
	for _, state := range []string{"completed", "failed", "canceled", "needs_operator", "waiting_human"} {
		if !IsTerminalRunState(state) {
			t.Fatalf("IsTerminalRunState(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"running", "ready", "blocked", ""} {
		if IsTerminalRunState(state) {
			t.Fatalf("IsTerminalRunState(%q) = true, want false", state)
		}
	}
}

func TestShouldCloseLaunchedBeforeTerminal(t *testing.T) {
	for _, state := range []string{"needs_operator", "waiting_human"} {
		if !ShouldCloseLaunchedBeforeTerminal(state) {
			t.Fatalf("ShouldCloseLaunchedBeforeTerminal(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"completed", "failed", "canceled", "running"} {
		if ShouldCloseLaunchedBeforeTerminal(state) {
			t.Fatalf("ShouldCloseLaunchedBeforeTerminal(%q) = true, want false", state)
		}
	}
}

func TestIsLaunchedJobDone(t *testing.T) {
	for _, state := range []string{"completed", "failed", "canceled", "skipped", "waiting_human"} {
		if !IsLaunchedJobDone(state) {
			t.Fatalf("IsLaunchedJobDone(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"queued", "claimed", "running"} {
		if IsLaunchedJobDone(state) {
			t.Fatalf("IsLaunchedJobDone(%q) = true, want false", state)
		}
	}
}

func TestIsPausedRun(t *testing.T) {
	if IsPausedRun(map[string]any{}) {
		t.Fatal("absent paused_at should not be paused")
	}
	if IsPausedRun(map[string]any{"paused_at": nil}) {
		t.Fatal("nil paused_at should not be paused")
	}
	if IsPausedRun(map[string]any{"paused_at": ""}) {
		t.Fatal("empty paused_at should not be paused")
	}
	if !IsPausedRun(map[string]any{"paused_at": "2026-06-14T00:00:00Z"}) {
		t.Fatal("set paused_at should be paused")
	}
	if !IsPausedRun(map[string]any{"paused_at": true}) {
		t.Fatal("non-string non-nil paused_at should be paused")
	}
}

func TestActiveSessionsBySlotAndIDs(t *testing.T) {
	sessions := []map[string]any{
		session("s1", "author", "codex", "active"),
		session("s2", "author", "codex", "active"),
		session("s3", "reviewer", "agy", "closed"),
		session("s4", "", "codex", "active"), // incomplete slot — skipped
	}
	bySlot := ActiveSessionsBySlot(sessions)
	if got := bySlot[RoleLane{Role: "author", Lane: "codex"}]; !reflect.DeepEqual(got, []string{"s1", "s2"}) {
		t.Fatalf("author/codex sessions = %#v, want [s1 s2]", got)
	}
	if _, ok := bySlot[RoleLane{Role: "reviewer", Lane: "agy"}]; ok {
		t.Fatal("closed session should not appear in active slot map")
	}
	ids := ActiveSessionIDs(sessions)
	if !ids["s1"] || !ids["s2"] || ids["s3"] || !ids["s4"] {
		t.Fatalf("active ids = %#v", ids)
	}
}

func TestNextUnusedSession(t *testing.T) {
	used := map[string]bool{"s1": true}
	if got := NextUnusedSession([]string{"s1", "s2"}, used); got != "s2" {
		t.Fatalf("NextUnusedSession = %q, want s2", got)
	}
	if got := NextUnusedSession([]string{"s1"}, used); got != "" {
		t.Fatalf("NextUnusedSession = %q, want empty", got)
	}
}

func TestResolveLanePrefersJobRow(t *testing.T) {
	j := map[string]any{"workflow_job_id": "x", "lane_id": "direct"}
	if lane, ok := ResolveLane(j, nil); !ok || lane != "direct" {
		t.Fatalf("ResolveLane = %q,%v, want direct,true", lane, ok)
	}
	j2 := map[string]any{"workflow_job_id": "x"}
	if _, ok := ResolveLane(j2, map[string]any{"jobs": []any{}}); ok {
		t.Fatal("ResolveLane should be ambiguous with no lane anywhere")
	}
}

func TestJobSlot(t *testing.T) {
	got := JobSlot(map[string]any{"workflow_job_id": "x", "attempt": 3})
	if got != (SlotKey{WorkflowJobID: "x", Attempt: 3}) {
		t.Fatalf("JobSlot = %#v", got)
	}
}

func TestIntValueCoercions(t *testing.T) {
	cases := map[any]int{int(1): 1, int32(2): 2, int64(3): 3, float64(4): 4, "5": 5, "x": 0, nil: 0}
	for in, want := range cases {
		if got := IntValue(in); got != want {
			t.Fatalf("IntValue(%v) = %d, want %d", in, got, want)
		}
	}
}
