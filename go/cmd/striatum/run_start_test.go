package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withStubbedRunStart swaps the start executor and the auto-drive launcher for
// the duration of fn, restoring them after. It lets us assert runRunStart's
// orchestration without a live daemon or systemd.
func withStubbedRunStart(t *testing.T, startCode int, fn func(launches *[]driverLaunch)) {
	t.Helper()
	origExec := runStartExecute
	origLaunch := autoDriveLaunch
	t.Cleanup(func() {
		runStartExecute = origExec
		autoDriveLaunch = origLaunch
	})

	var launches []driverLaunch
	runStartExecute = func(args []string, stdout, stderr io.Writer) int {
		// --no-drive must be stripped before the start mutation runs.
		for _, a := range args {
			if a == "--no-drive" {
				t.Errorf("--no-drive leaked into run.start args: %v", args)
			}
		}
		return startCode
	}
	autoDriveLaunch = func(stderr io.Writer, l driverLaunch) error {
		launches = append(launches, l)
		return nil
	}
	fn(&launches)
}

func TestRunRunStartAutoDrivesOnSuccess(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "--run-id", "run_abc123"},
			RepoPath:    "/tmp/target",
		}
		args := []string{"--repo", "/tmp/target", "run", "start", "--run-id", "run_abc123"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 1 {
			t.Fatalf("expected exactly one driver launch, got %d", len(*launches))
		}
		got := (*launches)[0]
		if got.RunID != "run_abc123" {
			t.Errorf("RunID = %q, want run_abc123", got.RunID)
		}
		if got.Repo != "/tmp/target" {
			t.Errorf("Repo = %q, want /tmp/target", got.Repo)
		}
	})
}

// TestRunRunStartAutoDrivesPositionalRunID is the #295 regression: `run start`
// accepts the run id POSITIONALLY (`run start <id>`, the run_start ParamsGroup
// maps the first positional to run_id), not only as `--run-id <id>`. Before the
// fix the auto-drive run-id derivation only read the --run-id flag, so the
// positional form silently skipped auto-drive — the run sat `running` with a
// claimable job and zero lanes, with no unit, hint, or error.
func TestRunRunStartAutoDrivesPositionalRunID(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "run_pos123"},
			RepoPath:    "/tmp/target",
		}
		args := []string{"--repo", "/tmp/target", "run", "start", "run_pos123"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 1 {
			t.Fatalf("positional run id must auto-drive, got %d launches", len(*launches))
		}
		if got := (*launches)[0].RunID; got != "run_pos123" {
			t.Errorf("RunID = %q, want run_pos123", got)
		}
	})
}

func TestRunRunStartAutoDriveAbsolutizesRelativeRepo(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		dir := t.TempDir()
		oldwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.Chdir(oldwd)
		})
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "--run-id", "run_rel123"},
			RepoPath:    ".",
		}
		code := runRunStart([]string{"--repo", ".", "run", "start", "--run-id", "run_rel123"}, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 1 {
			t.Fatalf("expected exactly one driver launch, got %d", len(*launches))
		}
		want := filepath.Clean(dir)
		if got := (*launches)[0].Repo; got != want {
			t.Fatalf("driver repo = %q, want %q", got, want)
		}
	})
}

// TestRunStartRunID covers both arg forms (and the flag-wins precedence) of the
// run-id derivation that feeds auto-drive (#295).
func TestRunStartRunID(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"flag space", []string{"--run-id", "r1"}, "r1"},
		{"flag equals", []string{"--run-id=r2"}, "r2"},
		{"positional", []string{"r3"}, "r3"},
		{"positional after valueless flag", []string{"--json", "r4"}, "r4"},
		{"flag wins over positional", []string{"r_pos", "--run-id", "r_flag"}, "r_flag"},
		{"value-flag not mistaken for positional", []string{"--interval", "15s"}, ""},
		{"none", []string{"--help"}, ""},
		{"empty", nil, ""},
	}
	for _, c := range cases {
		if got := runStartRunID(c.args); got != c.want {
			t.Errorf("%s: runStartRunID(%v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestRunRunStartNoDriveFlagOptsOut(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "--run-id", "run_abc123", "--no-drive"},
			RepoPath:    "/tmp/target",
		}
		args := []string{"--repo", "/tmp/target", "run", "start", "--run-id", "run_abc123", "--no-drive"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 0 {
			t.Fatalf("--no-drive must suppress auto-drive, got %d launches", len(*launches))
		}
	})
}

func TestRunRunStartEnvOptsOut(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "0")
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "--run-id", "run_abc123"},
		}
		args := []string{"run", "start", "--run-id", "run_abc123"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 0 {
			t.Fatalf("STRIATUM_RUN_DRIVE_AUTO=0 must suppress auto-drive, got %d", len(*launches))
		}
	})
}

func TestRunRunStartNoDriveOnFailedStart(t *testing.T) {
	withStubbedRunStart(t, 7, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		globals := leadingGlobals{
			CommandArgs: []string{"run", "start", "--run-id", "run_abc123"},
		}
		args := []string{"run", "start", "--run-id", "run_abc123"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 7 {
			t.Fatalf("failed start exit code must pass through, got %d", code)
		}
		if len(*launches) != 0 {
			t.Fatalf("a failed start must not auto-drive, got %d launches", len(*launches))
		}
	})
}

func TestRunRunStartNoRunIDNoDrive(t *testing.T) {
	withStubbedRunStart(t, 0, func(launches *[]driverLaunch) {
		t.Setenv(envRunDriveAuto, "")
		globals := leadingGlobals{CommandArgs: []string{"run", "start", "--help"}}
		args := []string{"run", "start", "--help"}
		code := runRunStart(args, io.Discard, io.Discard, globals)
		if code != 0 {
			t.Fatalf("expected exit 0, got %d", code)
		}
		if len(*launches) != 0 {
			t.Fatalf("no --run-id means nothing to drive, got %d launches", len(*launches))
		}
	})
}

func TestStripNoDrive(t *testing.T) {
	args := []string{"--repo", "/x", "run", "start", "--no-drive", "--run-id", "r1"}
	got, auto := stripNoDrive(args)
	if auto {
		t.Errorf("auto should be false when --no-drive present")
	}
	want := []string{"--repo", "/x", "run", "start", "--run-id", "r1"}
	if len(got) != len(want) {
		t.Fatalf("stripped args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stripped args = %v, want %v", got, want)
		}
	}
}

func TestFlagValue(t *testing.T) {
	cases := []struct {
		args []string
		name string
		want string
	}{
		{[]string{"--run-id", "r1"}, "run-id", "r1"},
		{[]string{"--run-id=r2"}, "run-id", "r2"},
		{[]string{"--other", "x"}, "run-id", ""},
		{[]string{"--run-id"}, "run-id", ""},
	}
	for _, c := range cases {
		if got := flagValue(c.args, c.name); got != c.want {
			t.Errorf("flagValue(%v, %q) = %q, want %q", c.args, c.name, got, c.want)
		}
	}
}

func TestAutoDriveEnabled(t *testing.T) {
	on := []string{"X=1"}
	if !autoDriveEnabled(on) {
		t.Errorf("default (unset) must be enabled")
	}
	for _, v := range []string{"0", "false", "no", "off", "OFF", " false "} {
		if autoDriveEnabled([]string{envRunDriveAuto + "=" + v}) {
			t.Errorf("%q must disable auto-drive", v)
		}
	}
	for _, v := range []string{"1", "true", "yes", "on"} {
		if !autoDriveEnabled([]string{envRunDriveAuto + "=" + v}) {
			t.Errorf("%q must keep auto-drive enabled", v)
		}
	}
}

func TestSanitizeUnit(t *testing.T) {
	if got := sanitizeUnit("run_abc-123.4"); got != "run_abc-123.4" {
		t.Errorf("valid id altered: %q", got)
	}
	if got := sanitizeUnit("run/abc 1"); got != "run-abc-1" {
		t.Errorf("sanitizeUnit = %q, want run-abc-1", got)
	}
}

// #513: the generated detached-driver unit must carry Restart=on-failure (plus a
// bounded restart loop) so a driver that exits non-zero on a daemon restart is
// self-healed by systemd instead of silently abandoning a live, resumable run.
func TestDriverUnitArgsCarriesRestartOnFailure(t *testing.T) {
	args := driverUnitArgs("striatum-drive-run_x", "/usr/local/bin/striatum", driverLaunch{
		RunID:      "run_x",
		Repo:       "/repo",
		SocketPath: "/run/striatum/rpc/daemon-go.sock",
		TokenFile:  "/run/tok",
	})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--property=Restart=on-failure",
		"--property=RestartSec=2s",
		"--property=StartLimitIntervalSec=120s",
		"--property=StartLimitBurst=10",
		"--collect",
		"run drive --run-id run_x",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("driver unit args missing %q; got %q", want, joined)
		}
	}
	// The run-drive verb must come AFTER the `--` separator so the properties are
	// not mistaken for striatum flags.
	sep := -1
	verb := -1
	for i, a := range args {
		if a == "--" && sep == -1 {
			sep = i
		}
		if a == "drive" {
			verb = i
		}
	}
	if sep == -1 || verb == -1 || verb < sep {
		t.Fatalf("expected `drive` after `--`; sep=%d verb=%d args=%#v", sep, verb, args)
	}
}
