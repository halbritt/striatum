package mutations

import (
	"context"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

// TestSupervisorHeartbeatCoalesceFloorEnv pins the env-var contract for the RFC
// 0139 Direction 1 write-skip floor: an empty env is the default, a parseable Go
// duration sets the floor, "off"/"0"/negative disables coalescing, and a typo
// falls back to the default (never silently disabling the skip).
func TestSupervisorHeartbeatCoalesceFloorEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want time.Duration
	}{
		{name: "unset_default", set: false, want: defaultSupervisorHeartbeatCoalesce},
		{name: "explicit_45s", env: "45s", set: true, want: 45 * time.Second},
		{name: "explicit_2m", env: "2m", set: true, want: 2 * time.Minute},
		{name: "zero_disables", env: "0", set: true, want: 0},
		{name: "off_disables", env: "off", set: true, want: 0},
		{name: "disable_word", env: "disable", set: true, want: 0},
		{name: "negative_disables", env: "-5s", set: true, want: 0},
		{name: "garbage_falls_back", env: "not-a-duration", set: true, want: defaultSupervisorHeartbeatCoalesce},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv(supervisorHeartbeatCoalesceEnv, c.env)
			} else {
				// t.Setenv with empty value still "sets" it; to test the unset path
				// explicitly clear it for this subtest.
				t.Setenv(supervisorHeartbeatCoalesceEnv, "")
			}
			if got := supervisorHeartbeatCoalesceFloor(); got != c.want {
				t.Fatalf("supervisorHeartbeatCoalesceFloor() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestSupervisorHeartbeatBumpSkipped pins the row-derived skip decision: a bump is
// skipped only when coalescing is enabled AND the prior timestamp is younger than
// the floor. An empty/unparseable prior timestamp (first write, unknown) never
// skips, and a disabled floor never skips.
func TestSupervisorHeartbeatBumpSkipped(t *testing.T) {
	now := "2026-06-19T12:00:30Z"
	cases := []struct {
		name     string
		floorEnv string
		prior    string
		wantSkip bool
	}{
		// 30s default floor; prior is 10s old → fresh → skip.
		{name: "fresh_within_floor_skips", floorEnv: "", prior: "2026-06-19T12:00:20Z", wantSkip: true},
		// prior is 40s old → older than the 30s floor → write.
		{name: "stale_beyond_floor_writes", floorEnv: "", prior: "2026-06-19T11:59:50Z", wantSkip: false},
		// exactly at the floor boundary (30s) → not younger than floor → write.
		{name: "exactly_floor_writes", floorEnv: "", prior: "2026-06-19T12:00:00Z", wantSkip: false},
		// empty prior (no stored timestamp / first bump) → always write.
		{name: "empty_prior_writes", floorEnv: "", prior: "", wantSkip: false},
		// coalescing disabled → never skip even with a fresh prior.
		{name: "disabled_never_skips", floorEnv: "off", prior: "2026-06-19T12:00:29Z", wantSkip: false},
		// a larger configured floor makes a once-stale prior fresh again.
		{name: "wider_floor_skips", floorEnv: "2m", prior: "2026-06-19T11:59:00Z", wantSkip: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(supervisorHeartbeatCoalesceEnv, c.floorEnv)
			if got := supervisorHeartbeatBumpSkipped(c.prior, now); got != c.wantSkip {
				t.Fatalf("supervisorHeartbeatBumpSkipped(prior=%q, now=%q) = %v, want %v", c.prior, now, got, c.wantSkip)
			}
		})
	}
}

// reportProgress drives a single helper progress event through HandleSuperviseReport
// against the in-memory fake, returning the fake tx so the caller can assert which
// heartbeat UPDATEs executed.
func reportProgress(t *testing.T, priorUpdatedAt string) *superviseReportFakeTx {
	t.Helper()
	tx := &superviseReportFakeTx{
		supervisor: supervisorReportRow{
			SupervisorID:       "sup_1",
			RunID:              "run_1",
			SessionID:          "sess_1",
			State:              "attached",
			DaemonSupervisorID: "dsup_1",
			PointerUpdatedAt:   priorUpdatedAt,
		},
	}
	runner := &superviseReportFakeRunner{tx: tx}
	if _, err := HandleSuperviseReport(context.Background(), runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "req_coalesce",
		Method:        "supervise.report",
		Params: map[string]any{
			"repository_id": "repo_1",
			"supervisor_id": "sup_1",
			"session_id":    "sess_1",
			// A bare (non-meaningful) progress event: it takes ONLY the
			// refreshReportSupervisorHeartbeat path, not the lease/liveness branch,
			// so the test isolates the coalesce decision on the timestamp bumps.
			"event_type": "progress",
			"payload":    map[string]any{"bytes": 1},
		},
	}); err != nil {
		t.Fatalf("HandleSuperviseReport: %v", err)
	}
	return tx
}

func sawHeartbeatBump(tx *superviseReportFakeTx) bool {
	return tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "updated_at") ||
		tx.sawExec("UPDATE striatumd.process_supervisors", "heartbeat_at") ||
		tx.sawExec("UPDATE striatumd.daemon_supervisors", "heartbeat_at")
}

// TestSuperviseReportCoalescesFreshHeartbeat is the RFC 0139 Direction 1 behavior:
// when the stored pointer timestamp is younger than the floor, a helper progress
// event skips all three timestamp UPDATEs (the write-amplification cut). The prior
// timestamp is read from the already-held row, so no extra SELECT is issued.
func TestSuperviseReportCoalescesFreshHeartbeat(t *testing.T) {
	t.Setenv(supervisorHeartbeatCoalesceEnv, "30s")
	// Prior pointer timestamp is 5s old → well within the 30s floor → skip.
	fresh := time.Now().UTC().Add(-5 * time.Second).Format(time.RFC3339)
	tx := reportProgress(t, fresh)
	if sawHeartbeatBump(tx) {
		t.Fatalf("fresh heartbeat (prior=%s) should have been coalesced (no timestamp UPDATE), execs=%#v", fresh, tx.execs)
	}
}

// TestSuperviseReportWritesStaleHeartbeat is the complementary case: a stored
// timestamp older than the floor still writes, keeping updated_at monotonic for the
// reconcile read's ORDER BY tiebreak.
func TestSuperviseReportWritesStaleHeartbeat(t *testing.T) {
	t.Setenv(supervisorHeartbeatCoalesceEnv, "30s")
	// Prior pointer timestamp is 10 minutes old → older than the floor → write.
	stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
	tx := reportProgress(t, stale)
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "updated_at") {
		t.Fatalf("stale heartbeat (prior=%s) should bump the pointer updated_at, execs=%#v", stale, tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.process_supervisors", "heartbeat_at") {
		t.Fatalf("stale heartbeat should bump process_supervisors.heartbeat_at, execs=%#v", tx.execs)
	}
	if !tx.sawExec("UPDATE striatumd.daemon_supervisors", "heartbeat_at") {
		t.Fatalf("stale heartbeat should bump daemon_supervisors.heartbeat_at, execs=%#v", tx.execs)
	}
}

// TestSuperviseReportDisabledCoalesceAlwaysWrites pins the env disable switch: with
// coalescing off, even a fresh prior timestamp writes (the pre-RFC-0139 behavior),
// so an operator can revert the throttle without a code change.
func TestSuperviseReportDisabledCoalesceAlwaysWrites(t *testing.T) {
	t.Setenv(supervisorHeartbeatCoalesceEnv, "off")
	fresh := time.Now().UTC().Add(-1 * time.Second).Format(time.RFC3339)
	tx := reportProgress(t, fresh)
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "updated_at") {
		t.Fatalf("coalesce disabled: fresh heartbeat must still write, execs=%#v", tx.execs)
	}
}

// TestSuperviseReportEmptyPriorWritesFirstHeartbeat guards the first-bump case: a
// row with no stored timestamp always gets its first write (the coalesce never
// strands a never-written liveness pulse).
func TestSuperviseReportEmptyPriorWritesFirstHeartbeat(t *testing.T) {
	t.Setenv(supervisorHeartbeatCoalesceEnv, "30s")
	tx := reportProgress(t, "")
	if !tx.sawExec("UPDATE striatumd.process_supervisor_pointers", "updated_at") {
		t.Fatalf("empty prior timestamp must write the first heartbeat, execs=%#v", tx.execs)
	}
}
