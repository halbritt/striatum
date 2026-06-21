package rundrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	"github.com/halbritt/striatum/go/pkg/laneproviderauth"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/runreconcile"
)

type Invoker interface {
	Invoke(context.Context, string, map[string]any) (map[string]any, error)
}

type Options struct {
	RepositoryID     string
	RunID            string
	RepoRoot         string
	Interval         time.Duration
	Once             bool
	JSON             bool
	Stdout           io.Writer
	Stderr           io.Writer
	Now              func() time.Time
	Sleep            func(context.Context, time.Duration) error
	PID              int
	ProviderAuthGate string
	// ForceConcurrent permits this drive to start even when a live drive for the
	// same run already holds the advisory marker. Default (false) refuses with a
	// clear "stop pid N first" so a stop/re-drive no longer leaves a stray
	// duplicate drive coexisting behind the daemon's double-claim guard (#293).
	// The intentional waiter composition (auto-drive in background + a foreground
	// `run drive` used purely as a terminal-state waiter) sets this true.
	ForceConcurrent bool
	// ReconnectBudget bounds how long an invoke retries a transient
	// `daemon_unreachable` error before giving up (#513). A daemon restart drops
	// the socket for a few seconds; without this the next invoke exits the driver
	// (status=11) and abandons a live, resumable run. Zero selects the default
	// (30s); a negative value disables reconnect (fail fast, prior behavior).
	ReconnectBudget time.Duration
	// ReconnectStep is the initial backoff between reconnect attempts; it doubles
	// up to ReconnectMaxStep. Zero selects the default (250ms).
	ReconnectStep time.Duration
	// ReconnectMaxStep caps the per-attempt backoff. Zero selects the default
	// (2s).
	ReconnectMaxStep time.Duration
}

// ConcurrentDriveError reports that a live `run drive` for the same run already
// holds the advisory marker, so a second drive was refused (#293 claim 2).
type ConcurrentDriveError struct {
	RunID string
	PID   int
}

func (e ConcurrentDriveError) Error() string {
	return fmt.Sprintf("run %s is already being driven by pid %d; stop it first (e.g. `kill %d`) or pass --force-concurrent to co-drive", e.RunID, e.PID, e.PID)
}

type Driver struct {
	invoker    Invoker
	options    Options
	launched   map[slotKey]string
	pausedHeld bool
}

type Action struct {
	TS            string `json:"ts,omitempty"`
	Action        string `json:"action"`
	RunID         string `json:"run_id,omitempty"`
	JobID         string `json:"job_id,omitempty"`
	WorkflowJobID string `json:"workflow_job_id,omitempty"`
	Attempt       int    `json:"attempt,omitempty"`
	Role          string `json:"role,omitempty"`
	Lane          string `json:"lane,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	Result        string `json:"result,omitempty"`
	Message       string `json:"message,omitempty"`
}

type TerminalError struct {
	RunID string
	State string
}

func (e TerminalError) Error() string {
	return fmt.Sprintf("run %s reached terminal state %s", e.RunID, e.State)
}

// ProviderAuthRefusalExitCode is the dedicated, NON-RESTARTABLE exit status the
// `run drive` command returns when a deterministic provider-auth preflight
// refusal stops the driver (#556). The detached systemd unit sets
// RestartPreventExitStatus to this code so a deterministic refusal does NOT
// crash-loop to the StartLimitBurst rate-limit (the bug that left the run
// running with no driver). It is distinct from the transient-socket exit (11),
// whose Restart=on-failure self-heal (#513) is preserved. 3 is unused by the
// CLI's exit-code catalog (rpcclient.exitCode uses 1,2,6,10,11,12,13).
const ProviderAuthRefusalExitCode = 3

// ProviderAuthRefusalError reports that the provider-auth preflight refused to
// launch a lane. It is DETERMINISTIC (the same auth gap refuses every pass), so
// the driver stops rather than thrashing, and the command exits with the
// non-restartable ProviderAuthRefusalExitCode (#556).
type ProviderAuthRefusalError struct {
	Code      string
	SessionID string
}

func (e ProviderAuthRefusalError) Error() string {
	if e.Code == "" {
		return "lane provider auth preflight refused launch"
	}
	return "lane provider auth preflight refused launch: " + e.Code
}

// slotKey / roleLane alias the shared reconcile predicate's types (RFC 0122):
// the launch decision lives in pkg/runreconcile so `run drive` and the daemon
// auto_spawn scheduler are one algorithm. The aliases keep this driver's
// call sites (and tests) reading naturally.
type slotKey = runreconcile.SlotKey

type roleLane = runreconcile.RoleLane

var AllowedMethods = map[string]bool{
	"run.detail":       true,
	"list.sessions":    true,
	"session.register": true,
	"session.close":    true,
	"supervise.start":  true,
	"supervise.stop":   true,
	"wake.wait":        true,
}

func New(invoker Invoker, options Options) *Driver {
	if options.Interval <= 0 {
		options.Interval = 15 * time.Second
	}
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	if options.Sleep == nil {
		options.Sleep = func(ctx context.Context, d time.Duration) error {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}
	if options.PID == 0 {
		options.PID = os.Getpid()
	}
	if strings.TrimSpace(options.ProviderAuthGate) == "" {
		options.ProviderAuthGate = "auto"
	}
	if options.ReconnectBudget == 0 {
		options.ReconnectBudget = 30 * time.Second
	}
	if options.ReconnectStep <= 0 {
		options.ReconnectStep = 250 * time.Millisecond
	}
	if options.ReconnectMaxStep <= 0 {
		options.ReconnectMaxStep = 2 * time.Second
	}
	return &Driver{
		invoker:  invoker,
		options:  options,
		launched: map[slotKey]string{},
	}
}

func Run(ctx context.Context, invoker Invoker, options Options) error {
	if strings.TrimSpace(options.RepositoryID) == "" {
		return fmt.Errorf("run drive requires repository_id")
	}
	if strings.TrimSpace(options.RunID) == "" {
		return fmt.Errorf("run drive requires run_id")
	}
	driver := New(invoker, options)
	cleanup, err := driver.claimAdvisoryMarker()
	if err != nil {
		return err
	}
	defer cleanup()
	for {
		actions, state, terminal, err := driver.ReconcileOnce(ctx)
		for _, action := range actions {
			driver.emit(action)
		}
		if len(actions) == 0 && driver.options.Once && driver.options.JSON {
			driver.emit(Action{
				TS:     driver.timestamp(),
				Action: "tick",
				RunID:  driver.options.RunID,
				Result: state,
			})
		}
		if err != nil {
			return err
		}
		if terminal {
			if state == "completed" {
				return nil
			}
			return TerminalError{RunID: options.RunID, State: state}
		}
		if driver.options.Once {
			return nil
		}
		if err := driver.waitForWake(ctx); err != nil {
			return err
		}
	}
}

func (d *Driver) waitForWake(ctx context.Context) error {
	timeoutMS := int(d.options.Interval / time.Millisecond)
	if timeoutMS <= 0 {
		timeoutMS = 1
	}
	_, err := d.invoke(ctx, "wake.wait", map[string]any{
		"run_id":     d.options.RunID,
		"timeout_ms": timeoutMS,
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return d.options.Sleep(ctx, d.options.Interval)
}

func (d *Driver) ReconcileOnce(ctx context.Context) ([]Action, string, bool, error) {
	detail, err := d.invoke(ctx, "run.detail", map[string]any{"run_id": d.options.RunID})
	if err != nil {
		return nil, "", false, err
	}
	run := asMap(detail["run"])
	state := stringValue(run["state"])
	if runreconcile.IsTerminalRunState(state) {
		actions := []Action{}
		if runreconcile.ShouldCloseLaunchedBeforeTerminal(state) && len(d.launched) > 0 {
			sessions, err := d.listSessions(ctx)
			if err != nil {
				return nil, state, false, err
			}
			actions = append(actions, d.forgetInactiveLaunched(sessions)...)
			actions = append(actions, d.closeLaunchedForTerminal(ctx, state)...)
		}
		actions = append(actions, d.action("terminal", Action{Result: state}))
		return actions, state, true, nil
	}
	jobs := mapList(detail["jobs"])
	workflow := asMap(detail["workflow"])
	sessions, err := d.listSessions(ctx)
	if err != nil {
		return nil, state, false, err
	}
	actions := []Action{}
	actions = append(actions, d.forgetInactiveLaunched(sessions)...)
	closeActions := d.closeFinishedLaunched(ctx, jobs)
	actions = append(actions, closeActions...)
	if anyStopped(closeActions) {
		sessions, err = d.listSessions(ctx)
		if err != nil {
			return actions, state, false, err
		}
	}
	// C5 (RFC 0124): a paused run must not get new lanes — the operator paused it
	// deliberately, and spawning past a pause is exactly the "spawn a job a human
	// meant to hold" hazard. Keep cleanup (forget/close) but skip launching queued
	// slots, staying non-terminal so resume re-drives. Announce once per paused
	// period to avoid journal spam over a long hold.
	if runreconcile.IsPausedRun(run) {
		if !d.pausedHeld {
			d.pausedHeld = true
			actions = append(actions, d.action("paused", Action{
				Result:  "paused",
				Message: "run is paused; holding (no new lanes launched until resume)",
			}))
		}
		return actions, state, false, nil
	}
	d.pausedHeld = false
	launchActions, err := d.launchQueued(ctx, jobs, workflow, sessions)
	actions = append(actions, launchActions...)
	if err != nil {
		return actions, state, false, err
	}
	return actions, state, false, nil
}

// forgetInactiveLaunched drops launched-slot memory for sessions the daemon no
// longer reports as active, so the slot becomes eligible for relaunch on this
// same pass. Without this, a session that dies while its job is (or returns
// to) queued — stale-lease requeue_same_attempt, a lane dying before claiming,
// pause/resume idle-exits, an adopted-but-ineligible session — would make
// launchQueued skip the slot forever. It must run before closeFinishedLaunched,
// against the same sessions snapshot launchQueued adopts from, so it never
// drops entries created later in the pass and closeFinishedLaunched never
// retries supervise.stop against an already-gone session.
func (d *Driver) forgetInactiveLaunched(sessions []map[string]any) []Action {
	active := runreconcile.ActiveSessionIDs(sessions)
	actions := []Action{}
	for key, sessionID := range d.launched {
		if active[sessionID] {
			continue
		}
		delete(d.launched, key)
		actions = append(actions, d.action("forget", Action{
			WorkflowJobID: key.WorkflowJobID,
			Attempt:       key.Attempt,
			SessionID:     sessionID,
			Result:        "forgotten",
			Message:       "launched session is no longer active; slot eligible for relaunch",
		}))
	}
	return actions
}

func (d *Driver) closeFinishedLaunched(ctx context.Context, jobs []map[string]any) []Action {
	actions := []Action{}
	seen := map[slotKey]bool{}
	for _, job := range jobs {
		key := runreconcile.JobSlot(job)
		seen[key] = true
		sessionID := d.launched[key]
		if sessionID == "" {
			continue
		}
		if !runreconcile.IsLaunchedJobDone(stringValue(job["state"])) {
			continue
		}
		action := d.jobAction("supervise.stop", job, Action{
			SessionID: sessionID,
			Result:    "attempted",
			Message:   "lane finished its job; closing for fresh-policy",
		})
		action = d.stopLaunchedSession(ctx, key, sessionID, action, "lane finished its job; closing for fresh-policy")
		actions = append(actions, action)
	}
	for key, sessionID := range d.launched {
		if seen[key] {
			continue
		}
		action := d.action("supervise.stop", Action{
			WorkflowJobID: key.WorkflowJobID,
			Attempt:       key.Attempt,
			SessionID:     sessionID,
			Result:        "attempted",
			Message:       "job attempt was superseded; closing launched lane",
		})
		action = d.stopLaunchedSession(ctx, key, sessionID, action, "job attempt was superseded; closing launched lane")
		actions = append(actions, action)
	}
	return actions
}

func (d *Driver) closeLaunchedForTerminal(ctx context.Context, state string) []Action {
	actions := []Action{}
	reason := fmt.Sprintf("run drive terminal cleanup: run reached %s", state)
	for key, sessionID := range d.launched {
		action := d.action("supervise.stop", Action{
			WorkflowJobID: key.WorkflowJobID,
			Attempt:       key.Attempt,
			SessionID:     sessionID,
			Result:        "attempted",
			Message:       reason,
		})
		action = d.stopLaunchedSession(ctx, key, sessionID, action, reason)
		actions = append(actions, action)
	}
	return actions
}

func (d *Driver) stopLaunchedSession(ctx context.Context, key slotKey, sessionID string, action Action, reason string) Action {
	if _, err := d.invoke(ctx, "supervise.stop", map[string]any{
		"session_id": sessionID,
		"reason":     reason,
	}); err != nil {
		action.Result = "error"
		action.Message = err.Error()
		return action
	}
	action.Result = "stopped"
	delete(d.launched, key)
	return action
}

func (d *Driver) launchQueued(ctx context.Context, jobs []map[string]any, workflow map[string]any, sessions []map[string]any) ([]Action, error) {
	actions := []Action{}
	// The launch decision is the shared reconcile predicate (RFC 0122): the same
	// PlanLaunch the daemon auto_spawn scheduler runs, so the two homes spawn
	// identically (contract C3). The driver only EXECUTES each decision's side
	// effects; it makes no spawn decision of its own.
	for _, decision := range runreconcile.PlanLaunch(jobs, workflow, sessions, d.launched) {
		switch decision.Kind {
		case runreconcile.LaunchAmbiguousLane:
			actions = append(actions, d.jobAction("skip", decision.Job, Action{
				Role:    decision.Role,
				Result:  "ambiguous_lane",
				Message: "cannot resolve lane for queued job; register manually",
			}))
		case runreconcile.LaunchUnadvanceable:
			// #389 gap 2: a non-terminal job the driver can neither launch nor track
			// (canonically a `blocked` job from a transient publish/seal failure) is
			// otherwise invisible — the driver would emit nothing and idle silently
			// while the run cannot progress. Surface it LOUDLY with the remediation
			// so the operator (or operator-model) is not left guessing. The driver
			// does NOT auto-recover; that stays an explicit operator `recovery` verb.
			actions = append(actions, d.jobAction("cannot_advance", decision.Job, Action{
				Role:    decision.Role,
				Result:  "cannot_advance_" + decision.State,
				Message: decision.Remediation,
			}))
		case runreconcile.LaunchAdoptExisting:
			d.launched[decision.Slot] = decision.SessionID
			actions = append(actions, d.jobAction("adopt", decision.Job, Action{
				Role:      decision.Role,
				Lane:      decision.Lane,
				SessionID: decision.SessionID,
				Result:    "adopted_existing_session",
			}))
		case runreconcile.LaunchRegisterFresh:
			launchActions, err := d.registerFreshLane(ctx, decision)
			actions = append(actions, launchActions...)
			if err != nil {
				return actions, err
			}
		}
	}
	return actions, nil
}

// registerFreshLane executes a LaunchRegisterFresh decision: register a fresh
// session for the slot and start its supervisor. A non-provider register/start
// failure is recorded and swallowed (the slot stays eligible for relaunch next
// pass); a provider-auth refusal is surfaced as ProviderAuthRefusalError so the
// driver stops fast, exactly as the pre-extraction launch loop did.
func (d *Driver) registerFreshLane(ctx context.Context, decision runreconcile.LaunchDecision) ([]Action, error) {
	actions := []Action{}
	role, lane := decision.Role, decision.Lane
	sessionID, registerActions, err := d.registerLane(ctx, role, lane)
	actions = append(actions, registerActions...)
	if err != nil {
		actions = append(actions, d.jobAction("register", decision.Job, Action{
			Role:    role,
			Lane:    lane,
			Result:  "error",
			Message: err.Error(),
		}))
		return actions, nil
	}
	if sessionID == "" {
		actions = append(actions, d.jobAction("register", decision.Job, Action{
			Role:    role,
			Lane:    lane,
			Result:  "waiting",
			Message: "fresh reviewer registration is blocked by an author still holding or eligible for work",
		}))
		return actions, nil
	}
	startAction := d.jobAction("supervise.start", decision.Job, Action{
		Role:      role,
		Lane:      lane,
		SessionID: sessionID,
		Result:    "attempted",
	})
	if _, err := d.invoke(ctx, "supervise.start", map[string]any{
		"session_id":         sessionID,
		"provider_auth_gate": d.options.ProviderAuthGate,
	}); err != nil {
		if code, ok := providerAuthFailureCode(err); ok {
			startAction.Result = "blocked"
			startAction.Message = "lane provider auth preflight refused launch: " + code
			actions = append(actions, startAction)
			_, _ = d.invoke(ctx, "session.close", map[string]any{
				"session_id": sessionID,
				"reason":     "run drive provider auth preflight refused launch: " + code,
			})
			return actions, ProviderAuthRefusalError{Code: code, SessionID: sessionID}
		}
		startAction.Result = "error"
		startAction.Message = err.Error()
		actions = append(actions, startAction)
		_, _ = d.invoke(ctx, "session.close", map[string]any{
			"session_id": sessionID,
			"reason":     "run drive supervise start failed",
		})
		return actions, nil
	}
	startAction.Result = "started"
	actions = append(actions, startAction)
	d.launched[decision.Slot] = sessionID
	return actions, nil
}

func (d *Driver) registerLane(ctx context.Context, role, lane string) (string, []Action, error) {
	actions := []Action{}
	for attempt := 0; attempt < 2; attempt++ {
		result, err := d.invoke(ctx, "session.register", map[string]any{
			"run_id": d.options.RunID,
			"role":   role,
			"lane":   lane,
			"fresh":  true,
		})
		if err == nil {
			sessionID := stringValue(result["session_id"])
			return sessionID, actions, nil
		}
		if attempt == 0 && isFreshPolicyRefusal(err) {
			closed, closeErr := d.closeCompletedSessions(ctx)
			actions = append(actions, closed...)
			if closeErr != nil {
				return "", actions, closeErr
			}
			continue
		}
		if isFreshPolicyRefusal(err) {
			return "", actions, nil
		}
		return "", actions, err
	}
	return "", actions, nil
}

func (d *Driver) closeCompletedSessions(ctx context.Context) ([]Action, error) {
	detail, err := d.invoke(ctx, "run.detail", map[string]any{"run_id": d.options.RunID})
	if err != nil {
		return nil, err
	}
	jobs := mapList(detail["jobs"])
	workflow := asMap(detail["workflow"])
	doneSlots := map[roleLane]bool{}
	for _, job := range jobs {
		if stringValue(job["state"]) == "completed" {
			lane, ok := runreconcile.ResolveLane(job, workflow)
			if !ok {
				continue
			}
			doneSlots[roleLane{Role: stringValue(job["role_id"]), Lane: lane}] = true
		}
	}
	sessions, err := d.listSessions(ctx)
	if err != nil {
		return nil, err
	}
	actions := []Action{}
	for _, session := range sessions {
		slot := roleLane{Role: stringValue(session["role_id"]), Lane: stringValue(session["lane_id"])}
		if stringValue(session["state"]) != "active" || !doneSlots[slot] {
			continue
		}
		sessionID := stringValue(session["session_id"])
		action := d.action("supervise.stop", Action{
			Role:      slot.Role,
			Lane:      slot.Lane,
			SessionID: sessionID,
			Result:    "attempted",
			Message:   "completed-job lane closed for fresh-policy",
		})
		if _, err := d.invoke(ctx, "supervise.stop", map[string]any{
			"session_id": sessionID,
			"reason":     "completed-job lane closed for fresh-policy",
		}); err != nil {
			action.Result = "error"
			action.Message = err.Error()
			actions = append(actions, action)
			continue
		}
		action.Result = "stopped"
		actions = append(actions, action)
	}
	return actions, nil
}

func (d *Driver) listSessions(ctx context.Context) ([]map[string]any, error) {
	result, err := d.invoke(ctx, "list.sessions", map[string]any{"run_id": d.options.RunID})
	if err != nil {
		return nil, err
	}
	return mapList(result["items"]), nil
}

func (d *Driver) invoke(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if !AllowedMethods[method] {
		return nil, fmt.Errorf("run drive attempted unsupported method %s", method)
	}
	next := map[string]any{"repository_id": d.options.RepositoryID}
	for key, value := range params {
		next[key] = value
	}
	return d.invokeWithReconnect(ctx, method, next)
}

// invokeWithReconnect calls the invoker, retrying transient `daemon_unreachable`
// errors with bounded exponential backoff (#513). A daemon restart drops the
// unix socket for a few seconds; without this the next invoke surfaces
// daemon_unreachable (exit 11) and the detached driver exits, abandoning a
// live, resumable run. We retry within ReconnectBudget (default 30s) so the
// socket can reappear, then give up so a genuinely dead daemon still fails
// loudly. ReconnectBudget < 0 disables reconnect (fail fast). Context
// cancellation/deadline is never retried.
func (d *Driver) invokeWithReconnect(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	deadline := d.options.Now().Add(d.options.ReconnectBudget)
	step := d.options.ReconnectStep
	announced := false
	for attempt := 0; ; attempt++ {
		result, err := d.invoker.Invoke(ctx, method, params)
		if err == nil || !isTransientUnreachable(err) || d.options.ReconnectBudget < 0 {
			return result, err
		}
		if ctx.Err() != nil {
			return result, err
		}
		if !d.options.Now().Before(deadline) {
			// Budget exhausted: the daemon did not come back. Surface the last
			// transient error so the caller exits with the normal unreachable code.
			return result, err
		}
		if !announced {
			announced = true
			_, _ = fmt.Fprintf(d.options.Stderr,
				"run drive: daemon unreachable on %s; reconnecting (up to %s) instead of exiting: %v\n",
				method, d.options.ReconnectBudget, err)
		}
		if sleepErr := d.options.Sleep(ctx, step); sleepErr != nil {
			return result, sleepErr
		}
		if step < d.options.ReconnectMaxStep {
			step *= 2
			if step > d.options.ReconnectMaxStep {
				step = d.options.ReconnectMaxStep
			}
		}
	}
}

// isTransientUnreachable reports whether err is a transient daemon-unreachable
// error worth retrying (the socket disappeared during a daemon restart and
// returns within seconds). It matches the rpcclient daemon_unreachable code and
// the rpc error code, plus the bare "no such file or directory" dial failure as
// a defensive string fallback for invokers that do not wrap a typed error.
func isTransientUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var clientErr *rpcclient.Error
	if errors.As(err, &clientErr) && clientErr.Code == "daemon_unreachable" {
		return true
	}
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) && rpcErr.Code == "daemon_unreachable" {
		return true
	}
	text := err.Error()
	return strings.Contains(text, "daemon unreachable") ||
		strings.Contains(text, "no such file or directory") ||
		strings.Contains(text, "connection refused")
}

func (d *Driver) emit(action Action) {
	if action.TS == "" {
		action.TS = d.timestamp()
	}
	if d.options.JSON {
		encoded, _ := json.Marshal(action)
		_, _ = fmt.Fprintln(d.options.Stdout, string(encoded))
		return
	}
	switch action.Action {
	case "terminal":
		_, _ = fmt.Fprintf(d.options.Stdout, "run %s -> %s\n", d.options.RunID, action.Result)
	case "skip":
		_, _ = fmt.Fprintf(d.options.Stderr, "skip %s: %s\n", action.WorkflowJobID, action.Message)
	case "cannot_advance":
		// #389 gap 2: never let a stuck run look like a quiet, healthy run. Emit a
		// loud, named warning to stderr (parallel to `skip`) so the operator sees
		// the un-advanceable job and its remediation instead of an empty log.
		_, _ = fmt.Fprintf(d.options.Stderr, "cannot advance %s (%s): %s\n", action.WorkflowJobID, action.Result, action.Message)
	default:
		_, _ = fmt.Fprintf(d.options.Stdout, "%s %s/%s %s attempt %d -> %s\n",
			action.Action, action.Role, action.Lane, action.WorkflowJobID, action.Attempt, action.Result)
	}
}

func (d *Driver) action(kind string, action Action) Action {
	action.TS = d.timestamp()
	action.Action = kind
	action.RunID = d.options.RunID
	return action
}

func (d *Driver) jobAction(kind string, job map[string]any, action Action) Action {
	action = d.action(kind, action)
	action.JobID = stringValue(job["job_id"])
	action.WorkflowJobID = stringValue(job["workflow_job_id"])
	action.Attempt = intValue(job["attempt"])
	if action.Role == "" {
		action.Role = stringValue(job["role_id"])
	}
	return action
}

func (d *Driver) timestamp() string {
	return d.options.Now().UTC().Format(time.RFC3339)
}

// claimAdvisoryMarker records this drive's pid in a per-run advisory marker and
// returns a cleanup that removes it. If a pre-existing marker names a *live*
// drive for the same run it REFUSES (returns ConcurrentDriveError) rather than
// coexisting behind the daemon's double-claim guard — a stop/re-drive otherwise
// leaves a stray duplicate drive an operator must hunt down and kill (#293).
// A marker that names a dead pid is reaped (overwritten). Set
// Options.ForceConcurrent to opt into co-driving (the documented background +
// foreground-waiter composition).
func (d *Driver) claimAdvisoryMarker() (func(), error) {
	if d.options.RepoRoot == "" || d.options.RunID == "" {
		return func() {}, nil
	}
	dir := filepath.Join(d.options.RepoRoot, ".striatum", "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		_, _ = fmt.Fprintf(d.options.Stderr, "warning: could not create run drive marker dir: %v\n", err)
		return func() {}, nil
	}
	path := filepath.Join(dir, "run-drive-"+safeFileComponent(d.options.RunID)+".pid")
	if prior, err := os.ReadFile(path); err == nil {
		fields := strings.Fields(string(prior))
		if len(fields) > 0 {
			if pid, err := strconv.Atoi(fields[0]); err == nil && pid > 0 && pid != d.options.PID && processAlive(pid) {
				if !d.options.ForceConcurrent {
					return func() {}, ConcurrentDriveError{RunID: d.options.RunID, PID: pid}
				}
				_, _ = fmt.Fprintf(d.options.Stderr, "warning: co-driving run %s with existing drive (pid %d); daemon session guards prevent duplicate claims\n", d.options.RunID, pid)
			}
		}
		// A stale marker (dead pid, our own pid, or unparseable) is reaped by the
		// write below.
	}
	content := fmt.Sprintf("%d\n", d.options.PID)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		_, _ = fmt.Fprintf(d.options.Stderr, "warning: could not write run drive marker: %v\n", err)
		return func() {}, nil
	}
	return func() {
		current, err := os.ReadFile(path)
		if err == nil && string(current) == content {
			_ = os.Remove(path)
		}
	}, nil
}

func anyStopped(actions []Action) bool {
	for _, action := range actions {
		if action.Result == "stopped" {
			return true
		}
	}
	return false
}

func isFreshPolicyRefusal(err error) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "reviewer_context_policy: fresh") ||
		strings.Contains(text, "--force-non-fresh")
}

func providerAuthFailureCode(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) && laneproviderauth.IsFailureClass(rpcErr.Code) {
		return rpcErr.Code, true
	}
	var clientErr *rpcclient.Error
	if errors.As(err, &clientErr) && laneproviderauth.IsFailureClass(clientErr.Code) {
		return clientErr.Code, true
	}
	for _, code := range []string{
		laneproviderauth.FailureAuthFailed,
		laneproviderauth.FailureBinaryMissing,
		laneproviderauth.FailureUnavailable,
		laneproviderauth.FailureTimeout,
		laneproviderauth.FailureLaunchFailed,
		laneproviderauth.FailureUnsupported,
		laneproviderauth.FailureUnexpectedResult,
	} {
		if strings.Contains(err.Error(), code) {
			return code, true
		}
	}
	return "", false
}

func safeFileComponent(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "run"
	}
	return b.String()
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func asMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func asList(value any) []any {
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

func mapList(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, asMap(item))
		}
		return result
	default:
		return nil
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intValue(value any) int {
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
