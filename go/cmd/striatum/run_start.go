package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Auto-drive (issue #212, the operator-token-burn / yolo-control-surface half of
// RFC 0122's do-now track): a successful `run start` launches a detached
// `run drive` for the run it just started, so the run reconciles itself to a
// terminal state with no operator process — and no operator-model tokens — in
// the loop. The driver is a deterministic Go loop, so the mechanical
// register-session + supervise.start work leaves the (expensive) operator model
// entirely.
//
// This only ever auto-drives a run an operator *deliberately started this
// instant*, so it cannot drive a paused or held run (the hazard a blind
// all-runs supervisor would hit, and the same one RFC 0122 §"crash/restart"
// guards). It is best-effort: a missing `systemd-run`, or any launch failure,
// degrades to a printed hint and never changes the start's exit code.
//
// Opt out per-invocation with `run start --no-drive`, or globally by exporting
// STRIATUM_RUN_DRIVE_AUTO=0 (also accepts false/no/off).
const envRunDriveAuto = "STRIATUM_RUN_DRIVE_AUTO"

// runStartExecute performs the actual `run start` mutation. It is the normal
// daemon route; it is a package var so tests can drive runRunStart without a
// live daemon.
var runStartExecute = runDaemonRoute

// autoDriveLaunch starts the detached driver for runID. It is a package var so
// tests can assert the launch decision without spawning systemd.
var autoDriveLaunch = launchDetachedDriver

// runRunStart wraps `run start`: it runs the start verbatim through the daemon
// route, then (on success, unless opted out) launches a detached driver for the
// started run. The start's stdout/stderr and exit code pass through unchanged;
// auto-drive notices go to stderr only, so `--json` consumers are unaffected.
func runRunStart(args []string, stdout, stderr io.Writer, globals leadingGlobals) int {
	startArgs, autoDrive := stripNoDrive(args)
	code := runStartExecute(startArgs, stdout, stderr)
	if code != 0 || !autoDrive || !autoDriveEnabled(os.Environ()) {
		return code
	}
	runID := runStartRunID(globals.CommandArgs[2:])
	if runID == "" {
		// `run start` without a run id either failed above or is a help/usage
		// path; nothing to drive.
		return code
	}
	repo := globals.RepoPath
	if resolvedRepo, err := clientRepoRoot(repo); err == nil {
		repo = resolvedRepo
	}
	if err := autoDriveLaunch(stderr, driverLaunch{
		RunID:      runID,
		Repo:       repo,
		SocketPath: globals.SocketPath,
		TokenFile:  globals.TokenFile,
		Env:        os.Environ(),
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "run start: auto-drive not launched (%v); drive manually with: striatum --repo %s run drive --run-id %s\n", err, repo, runID)
	}
	return code
}

// stripNoDrive removes the CLI-only `--no-drive` token (it is not a daemon
// param, so it must never reach run.start) and reports whether auto-drive
// remains enabled for this invocation.
func stripNoDrive(args []string) ([]string, bool) {
	autoDrive := true
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--no-drive" {
			autoDrive = false
			continue
		}
		out = append(out, a)
	}
	return out, autoDrive
}

// autoDriveEnabled honors the global STRIATUM_RUN_DRIVE_AUTO opt-out. Default is
// on; only an explicit falsey value disables it.
func autoDriveEnabled(env []string) bool {
	switch strings.ToLower(strings.TrimSpace(envLookup(env, envRunDriveAuto))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// flagValue returns the value of --name from a flag slice, supporting both
// `--name value` and `--name=value`.
func flagValue(args []string, name string) string {
	want := "--" + name
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == want {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if v, ok := strings.CutPrefix(a, want+"="); ok {
			return v
		}
	}
	return ""
}

// runStartRunID derives the run id from the args that follow `run start`,
// supporting BOTH the `--run-id <id>` / `--run-id=<id>` flag form and the bare
// positional form (`run start <id>`), which `run start` also accepts (the
// run_start ParamsGroup maps the first positional to run_id, and `--help`
// advertises `<run-id>`). The flag form wins when present; otherwise the first
// non-flag token is taken as the run id. Without this fallback the positional
// form silently skipped auto-drive (#295): the run sat `running` with a
// claimable job and zero lanes, with no unit, hint, or error.
func runStartRunID(args []string) string {
	if v := flagValue(args, "run-id"); v != "" {
		return v
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			return a
		}
		// Skip a flag that consumes the next token as its value, so we do not
		// mistake that value for the positional run id. `--no-drive` is already
		// stripped before the start mutation, and `--json` is valueless; any
		// other `--flag value` pair (e.g. `--run-id` handled above, or future
		// flags) must not have its value misread as a positional.
		if !strings.Contains(a, "=") && a != "--json" && i+1 < len(args) {
			i++
		}
	}
	return ""
}

type driverLaunch struct {
	RunID      string
	Repo       string
	SocketPath string
	TokenFile  string
	Env        []string
}

// launchDetachedDriver starts `run drive` for the run in a transient systemd
// user unit, so it survives this CLI process and is inspectable via
// `journalctl --user -u <unit>`. The unit name is derived from the run id, which
// makes the launch idempotent: a second start of an already-driven run fails the
// systemd-run "unit already exists" path, which we treat as success (already
// driving). Requires a systemd user session — the same one the daemon runs in;
// without `systemd-run` on PATH it returns an error and the caller prints a
// manual-drive hint.
func launchDetachedDriver(stderr io.Writer, l driverLaunch) error {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "striatum"
	}
	systemdRun, err := exec.LookPath("systemd-run")
	if err != nil {
		return fmt.Errorf("systemd-run not found")
	}
	unit := "striatum-drive-" + sanitizeUnit(l.RunID)

	cmd := []string{
		"--user", "--quiet",
		"--unit=" + unit,
		"--description=striatum run drive for " + l.RunID,
		"--collect", // garbage-collect the transient unit once it exits
	}
	// Carry the daemon socket override (if the operator set one in their env) into
	// the user-manager environment the driver inherits; the runtime client-token
	// is discovered from XDG_RUNTIME_DIR, which user units already have.
	if sock := envLookup(l.Env, "STRIATUM_DAEMON_SOCKET"); sock != "" {
		cmd = append(cmd, "--setenv=STRIATUM_DAEMON_SOCKET="+sock)
	}
	cmd = append(cmd, "--", bin)
	if l.Repo != "" {
		cmd = append(cmd, "--repo", l.Repo)
	}
	if l.SocketPath != "" {
		cmd = append(cmd, "--daemon-socket", l.SocketPath)
	}
	if l.TokenFile != "" {
		cmd = append(cmd, "--capability-token-file", l.TokenFile)
	}
	cmd = append(cmd, "run", "drive", "--run-id", l.RunID)

	run := exec.Command(systemdRun, cmd...)
	out, err := run.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if strings.Contains(trimmed, "already exists") {
			_, _ = fmt.Fprintf(stderr, "run start: run %s is already being driven (unit %s)\n", l.RunID, unit)
			return nil
		}
		if trimmed != "" {
			return fmt.Errorf("%s: %s", err, trimmed)
		}
		return err
	}
	// The unit is registered with --collect, so `systemctl --user stop` does
	// not just stop it: it removes the transient unit entirely. Resuming is
	// therefore `striatum run drive`, NOT `systemctl --user start` (which fails
	// "Unit ... not found"). Surface the accurate resume command up front (#293).
	resume := "striatum"
	if l.Repo != "" {
		resume = "striatum --repo " + l.Repo
	}
	_, _ = fmt.Fprintf(stderr, "run start: auto-driving %s in background (logs: journalctl --user -u %s -f; stop: systemctl --user stop %s; resume after stop: %s run drive --run-id %s)\n", l.RunID, unit, unit, resume, l.RunID)
	return nil
}

// sanitizeUnit keeps only characters valid in a systemd unit name; run ids are
// already in that set, but defend against surprises.
func sanitizeUnit(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
