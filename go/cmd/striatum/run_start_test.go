package main

import (
	"io"
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
