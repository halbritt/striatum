// Package runreconcile holds the launch-decision predicate shared by the
// operator-side `run drive` loop (go/pkg/cli/rundrive) and the daemon-side
// `supervision.auto_spawn` scheduler (RFC 0122). Keeping the decision in one
// pure, I/O-free place is what makes the two homes provably one algorithm
// (RFC 0122 §"The reconcile predicate is one algorithm in two homes"; contract
// C3 predicate parity): a scheduler-initiated spawn must make the identical
// launch decision the operator-driven loop would make on the same run state.
//
// Nothing here performs RPC or mutates its inputs. Both callers gather run
// state through their own transport (the CLI driver via run.detail/list.sessions
// RPCs; the daemon scheduler via direct reads), call the predicate, then execute
// the returned decisions. The package depends only on the standard library so
// the daemon (pkg/mutations) can import it without an import cycle through the
// CLI.
package runreconcile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SlotKey identifies a single job-attempt slot. A launched lane is tracked per
// (workflow_job_id, attempt) so a stale-lease requeue of the same attempt and a
// fresh next-attempt are distinct slots.
type SlotKey struct {
	WorkflowJobID string
	Attempt       int
}

// RoleLane identifies a role+lane pairing — the granularity at which an existing
// active session can be adopted for a queued job.
type RoleLane struct {
	Role string
	Lane string
}

// LaunchKind enumerates the decision the predicate reaches for one queued job.
type LaunchKind int

const (
	// LaunchAmbiguousLane: the job's lane cannot be resolved from the job row or
	// the workflow snapshot, so neither home can spawn it automatically.
	LaunchAmbiguousLane LaunchKind = iota
	// LaunchAdoptExisting: an unused active session already serves this job's
	// role+lane slot, so the caller adopts it instead of registering a new one.
	LaunchAdoptExisting
	// LaunchRegisterFresh: no adoptable session exists; the caller must register
	// a fresh session for the slot and start its supervisor.
	LaunchRegisterFresh
	// LaunchUnadvanceable: the job is non-terminal but the driver cannot advance
	// it this pass — it is neither queued (so PlanLaunch will not launch it) nor
	// in-flight on a live lane (so the launched-tracking / recovery paths do not
	// own it). The canonical case is a `blocked` job left by a transient
	// publish/seal failure (#389 gap 2): without this decision the driver simply
	// emits nothing and idles silently while the run makes no progress. The
	// caller surfaces it as a VISIBLE "cannot advance" signal naming the
	// remediation; the predicate never auto-recovers it.
	LaunchUnadvanceable
)

// LaunchDecision is the predicate's verdict for one queued job. The caller maps
// each decision onto its own side effects (emit a skip note; record an adoption;
// register+start a fresh lane) — the predicate itself stays pure.
type LaunchDecision struct {
	Job       map[string]any
	Slot      SlotKey
	Role      string
	Lane      string
	Kind      LaunchKind
	SessionID string // populated only for LaunchAdoptExisting
	// State is the job's current state. Populated for LaunchUnadvanceable so the
	// caller can name the stuck state in the operator-facing signal.
	State string
	// Remediation is a one-line, copy-pasteable hint naming the verb(s) that
	// unstick the job. Populated for LaunchUnadvanceable.
	Remediation string
}

// PlanLaunch decides, for every queued job not already launched, what the driver
// or scheduler should do this pass. It mirrors the legacy rundrive launchQueued
// loop exactly: iterate jobs in deterministic slot order, skip non-queued jobs
// and slots already launched, resolve the lane (skip as ambiguous when it cannot
// be resolved), adopt an unused active session for the role+lane slot when one
// exists, otherwise register a fresh session.
//
// launched maps already-launched slots to their session id; those sessions are
// excluded from adoption (a session already claimed by one slot is never adopted
// by another). PlanLaunch tracks adoptions it makes within the pass so two
// queued jobs sharing a role+lane never adopt the same session. It mutates
// neither launched nor any input row.
//
// Concurrency caps (#322): the workflow's top-level parallelism.max_active_jobs
// caps the total number of concurrently active lanes, and an implicit per-lane
// cap of 1 keeps two jobs from ever sharing a lane concurrently (which
// re-triggers the #290/#302 same-lane wedges). Both caps count the in-flight job
// rows (claimed/running/stale_lease) ∪ already-launched slots ∪ launch decisions
// emitted earlier in this pass, deduped by slot. AmbiguousLane decisions are
// skips, not launches: they are still emitted but consume no cap budget. Capping
// mid-loop is deterministic because SortedJobs orders the jobs deterministically.
func PlanLaunch(jobs []map[string]any, workflow map[string]any, sessions []map[string]any, launched map[SlotKey]string) []LaunchDecision {
	activeBySlot := ActiveSessionsBySlot(sessions)
	used := map[string]bool{}
	for _, sessionID := range launched {
		used[sessionID] = true
	}

	maxActive := MaxActiveJobs(workflow)
	// inFlightSlots seeds the active-lane accounting with the in-flight job rows
	// and any slots already launched this drive; occupiedLanes seeds the per-lane
	// cap with the lanes those slots already hold. Both grow as this pass emits
	// real launch decisions.
	inFlightSlots := map[SlotKey]bool{}
	occupiedLanes := map[string]bool{}
	for _, job := range jobs {
		if !inFlightJobStates[StringValue(job["state"])] {
			continue
		}
		slot := JobSlot(job)
		inFlightSlots[slot] = true
		if lane, ok := ResolveLane(job, workflow); ok {
			occupiedLanes[lane] = true
		}
	}
	jobBySlot := map[SlotKey]map[string]any{}
	for _, job := range jobs {
		jobBySlot[JobSlot(job)] = job
	}
	for slot := range launched {
		if !inFlightSlots[slot] {
			inFlightSlots[slot] = true
			if job := jobBySlot[slot]; job != nil {
				if lane, ok := ResolveLane(job, workflow); ok {
					occupiedLanes[lane] = true
				}
			}
		}
	}

	decisions := []LaunchDecision{}
	for _, job := range SortedJobs(jobs) {
		state := StringValue(job["state"])
		if state != "queued" {
			// #389 gap 2: a non-terminal job the driver can neither launch (not
			// queued) nor track as in-flight (no live lane) — canonically a
			// `blocked` job left by a transient publish/seal failure — is otherwise
			// invisible: the loop just `continue`s and the driver emits nothing,
			// idling silently while the run cannot progress. Emit a visible
			// "cannot advance" decision naming the remediation. This is a SIGNAL,
			// not a launch: it consumes no cap budget and never auto-recovers.
			if UnadvanceableJobState(state) {
				decisions = append(decisions, LaunchDecision{
					Job:         job,
					Slot:        JobSlot(job),
					Role:        StringValue(job["role_id"]),
					Kind:        LaunchUnadvanceable,
					State:       state,
					Remediation: UnadvanceableRemediation(job),
				})
			}
			continue
		}
		slot := JobSlot(job)
		if launched[slot] != "" {
			continue
		}
		// GLOBAL CAP: stop emitting launches once the active-lane budget is full.
		// AmbiguousLane skips below do not consume budget, so this check guards
		// only the real launch decisions, which all sit past it.
		if maxActive > 0 && len(inFlightSlots) >= maxActive {
			break
		}
		role := StringValue(job["role_id"])
		lane, ok := ResolveLane(job, workflow)
		if !ok {
			decisions = append(decisions, LaunchDecision{
				Job:  job,
				Slot: slot,
				Role: role,
				Kind: LaunchAmbiguousLane,
			})
			continue
		}
		// PER-LANE CAP of 1: never put two jobs on one lane concurrently.
		if occupiedLanes[lane] {
			continue
		}
		roleLane := RoleLane{Role: role, Lane: lane}
		if sessionID := NextUnusedSession(activeBySlot[roleLane], used); sessionID != "" {
			used[sessionID] = true
			inFlightSlots[slot] = true
			occupiedLanes[lane] = true
			decisions = append(decisions, LaunchDecision{
				Job:       job,
				Slot:      slot,
				Role:      role,
				Lane:      lane,
				Kind:      LaunchAdoptExisting,
				SessionID: sessionID,
			})
			continue
		}
		inFlightSlots[slot] = true
		occupiedLanes[lane] = true
		decisions = append(decisions, LaunchDecision{
			Job:  job,
			Slot: slot,
			Role: role,
			Lane: lane,
			Kind: LaunchRegisterFresh,
		})
	}
	return decisions
}

// inFlightJobStates is the set of job states that occupy a live lane — the
// counterpart of terminalJobStates in the daemon's recovery code and the
// running/claimed/stale_lease set complete-stalled finalizes. queued and blocked
// jobs are NOT in-flight (counting them would deadlock a run that never starts).
var inFlightJobStates = map[string]bool{
	"claimed":     true,
	"running":     true,
	"stale_lease": true,
}

// unadvanceableJobStates is the set of non-terminal job states the driver can
// neither launch (PlanLaunch only launches `queued`) nor own as in-flight work
// (those are handled by launched-tracking + the daemon recovery sweep). A job in
// one of these states blocks the run with no operator-facing signal unless the
// driver explicitly surfaces it (#389 gap 2). Today the only such state is
// `blocked`; `waiting_human` is a deliberate operator hand-off that flips the
// RUN to a drive-terminal state, so it is intentionally excluded.
var unadvanceableJobStates = map[string]bool{
	"blocked": true,
}

// UnadvanceableJobState reports whether a job in this state is non-terminal yet
// un-launchable and not in-flight — i.e. the driver cannot advance it and must
// surface it instead of idling silently.
func UnadvanceableJobState(state string) bool {
	return unadvanceableJobStates[state]
}

// UnadvanceableRemediation returns the one-line, copy-pasteable hint naming the
// recovery verb(s) that unstick a job in the given unadvanceable state. It is
// purely advisory legibility — it changes no behavior and triggers no
// auto-recovery.
//
// For `blocked` jobs the hint branches on whether the job ever started (non-nil
// started_at): a job that started means the lane finished but the artifact seal
// failed (#389 path); a job that never started is still waiting on an upstream
// dependency and must not claim a seal failure occurred.
func UnadvanceableRemediation(job map[string]any) string {
	state := StringValue(job["state"])
	switch state {
	case "blocked":
		if job["started_at"] != nil && job["started_at"] != "" {
			// The canonical #389 path: a transient publish/seal failure left the
			// work done-and-durable on disk but the job `blocked`. `recovery
			// reseal` moves it blocked -> queued on the same attempt; `recovery
			// resolve-blocker` clears a dangling blocker that no completion path
			// cleared.
			return "job is blocked and cannot be driven; run `striatum recovery reseal --run-id <run> --job-id <job>` (lane finished but the seal failed) or `striatum recovery resolve-blocker <blocker-id>`"
		}
		// The job has never started: it is waiting on an upstream dependency
		// that has not yet completed. `recovery resolve-blocker` clears a
		// dangling blocker; inspect with `striatum why` to identify the
		// unsatisfied dependency.
		return "job is blocked waiting on an upstream dependency; inspect with `striatum why` to identify the unsatisfied dependency or run `striatum recovery resolve-blocker <blocker-id>` if a blocker is dangling"
	default:
		return "job is " + state + " and cannot be driven; inspect with `striatum doctor` / `striatum why` and recover via the `striatum recovery` verbs"
	}
}

// MaxActiveJobs reads the workflow's top-level parallelism.max_active_jobs cap.
// It returns 0 (meaning unlimited) when the field is absent, zero, or
// non-positive, so workflows that never set it keep their pre-#322 behavior.
func MaxActiveJobs(workflow map[string]any) int {
	max := IntValue(AsMap(workflow["parallelism"])["max_active_jobs"])
	if max < 0 {
		return 0
	}
	return max
}

// IsTerminalRunState reports whether the driver/scheduler should stop driving a
// run. Note this is the DRIVE-loop notion of terminal (it includes the operator
// hand-off states needs_operator / waiting_human), which is broader than the
// run-lifecycle terminal set used inside the daemon's run mutations.
func IsTerminalRunState(state string) bool {
	switch state {
	case "completed", "failed", "canceled", "needs_operator", "waiting_human":
		return true
	default:
		return false
	}
}

// ShouldCloseLaunchedBeforeTerminal reports whether launched lanes must be
// closed before the loop returns on a terminal state — true for the operator
// hand-off states, where lanes are torn down so a human takes a clean slot.
func ShouldCloseLaunchedBeforeTerminal(state string) bool {
	switch state {
	case "needs_operator", "waiting_human":
		return true
	default:
		return false
	}
}

// IsLaunchedJobDone reports whether a launched lane's job has reached a state in
// which its lane should be closed for fresh-policy.
func IsLaunchedJobDone(state string) bool {
	switch state {
	case "completed", "failed", "canceled", "skipped", "waiting_human":
		return true
	default:
		return false
	}
}

// IsPausedRun reports whether the run row carries a non-null paused_at — a run
// the operator paused via run.pause. The daemon's run.detail returns SELECT *
// over runs, so paused_at is present whenever set (a SQL NULL decodes to a nil
// interface across the RPC boundary), and the daemon scheduler reads the same
// column directly. C5 (RFC 0124): a paused run must not get new lanes.
func IsPausedRun(run map[string]any) bool {
	v, ok := run["paused_at"]
	if !ok || v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}

// ActiveSessionsBySlot groups active session ids by their role+lane slot.
func ActiveSessionsBySlot(sessions []map[string]any) map[RoleLane][]string {
	result := map[RoleLane][]string{}
	for _, session := range sessions {
		if StringValue(session["state"]) != "active" {
			continue
		}
		slot := RoleLane{Role: StringValue(session["role_id"]), Lane: StringValue(session["lane_id"])}
		if slot.Role == "" || slot.Lane == "" {
			continue
		}
		result[slot] = append(result[slot], StringValue(session["session_id"]))
	}
	return result
}

// ActiveSessionIDs returns the set of active session ids — the live-session
// check both homes use to forget a launched slot whose session is gone.
func ActiveSessionIDs(sessions []map[string]any) map[string]bool {
	result := map[string]bool{}
	for _, session := range sessions {
		if StringValue(session["state"]) != "active" {
			continue
		}
		if sessionID := StringValue(session["session_id"]); sessionID != "" {
			result[sessionID] = true
		}
	}
	return result
}

// NextUnusedSession returns the first session id in sessions not already in used,
// or "" when every candidate is taken.
func NextUnusedSession(sessions []string, used map[string]bool) string {
	for _, sessionID := range sessions {
		if sessionID != "" && !used[sessionID] {
			return sessionID
		}
	}
	return ""
}

// SortedJobs returns jobs ordered deterministically by (workflow_job_id,
// attempt) so the launch decision is stable across passes and across homes.
func SortedJobs(jobs []map[string]any) []map[string]any {
	result := append([]map[string]any(nil), jobs...)
	sort.SliceStable(result, func(i, j int) bool {
		a := StringValue(result[i]["workflow_job_id"])
		b := StringValue(result[j]["workflow_job_id"])
		if a == b {
			return IntValue(result[i]["attempt"]) < IntValue(result[j]["attempt"])
		}
		return a < b
	})
	return result
}

// ResolveLane resolves a job's lane id from the job row's lane_id, its
// lane_selector_json, or — failing both — the workflow snapshot's jobs list.
func ResolveLane(job map[string]any, workflow map[string]any) (string, bool) {
	if lane := StringValue(job["lane_id"]); lane != "" {
		return lane, true
	}
	if lane := StringValue(AsMap(job["lane_selector_json"])["lane_id"]); lane != "" {
		return lane, true
	}
	workflowJobID := StringValue(job["workflow_job_id"])
	for _, item := range AsList(workflow["jobs"]) {
		wfJob := AsMap(item)
		if StringValue(wfJob["id"]) == workflowJobID {
			if lane := StringValue(wfJob["lane_id"]); lane != "" {
				return lane, true
			}
		}
	}
	return "", false
}

// JobSlot returns the (workflow_job_id, attempt) slot key for a job row.
func JobSlot(job map[string]any) SlotKey {
	return SlotKey{WorkflowJobID: StringValue(job["workflow_job_id"]), Attempt: IntValue(job["attempt"])}
}

// AsMap coerces an any into a map[string]any, returning an empty map otherwise.
func AsMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

// AsList coerces an any into a []any, tolerating a []map[string]any source.
func AsList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result
	default:
		return nil
	}
}

// StringValue renders a value as a trimmed string ("" for nil).
func StringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// IntValue coerces a value to an int across the numeric/string encodings that
// cross the RPC boundary.
func IntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(typed)
		return parsed
	default:
		return 0
	}
}
