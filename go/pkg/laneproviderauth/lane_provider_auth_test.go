package laneproviderauth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildCodexSmokeCommand(t *testing.T) {
	got := BuildCodexSmokeCommand(CodexSmokeOptions{
		Binary:     "/opt/codex/bin/codex",
		CWD:        "/tmp/preflight",
		OutputPath: "/tmp/preflight/out.txt",
	})
	want := []string{
		"/opt/codex/bin/codex",
		"exec",
		"--ignore-user-config",
		"--ignore-rules",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"-c", `approval_policy="never"`,
		"-C", "/tmp/preflight",
		"--output-last-message", "/tmp/preflight/out.txt",
		"--json",
		"Reply exactly: ok",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("command = %#v, want %#v", got, want)
	}
}

func TestSanitizeEnvAndLaunchSpecUseEnvI(t *testing.T) {
	env := SanitizeEnv([]string{
		"HOME=/home/lane",
		"USER=lane",
		"PATH=/usr/bin",
		"STRIATUM_MCP_TOKEN=secret",
		"DATABASE_URL=postgres://secret",
		"OPENAI_API_KEY=secret",
		"LC_ALL=C.UTF-8",
	}, []string{"/opt/codex/bin"})
	rendered := strings.Join(env, "\n")
	for _, forbidden := range []string{"STRIATUM_MCP_TOKEN", "DATABASE_URL", "OPENAI_API_KEY", "secret"} {
		if strings.Contains(rendered, forbidden) {
			t.Fatalf("sanitized env leaked %q: %s", forbidden, rendered)
		}
	}
	if !strings.Contains(rendered, "HOME=/home/lane") || !strings.Contains(rendered, "LC_ALL=C.UTF-8") {
		t.Fatalf("sanitized env dropped required basics: %s", rendered)
	}
	if !strings.Contains(rendered, "PATH=/opt/codex/bin:/usr/bin") {
		t.Fatalf("path_prefix was not prepended: %s", rendered)
	}

	spec := BuildLaunchSpec([]string{"codex", "exec"}, "/tmp/preflight", "striatum-lane", env)
	if spec.Name != "sudo" {
		t.Fatalf("run-as launch command = %q, want sudo", spec.Name)
	}
	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{"-n", "-u", "striatum-lane", "env", "-i", "HOME=/home/lane", "codex", "exec"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("run-as args missing %q: %#v", want, spec.Args)
		}
	}
	if strings.Contains(joined, "STRIATUM_MCP_TOKEN") || strings.Contains(joined, "DATABASE_URL") {
		t.Fatalf("run-as env-i args leaked secret env: %#v", spec.Args)
	}
}

func TestCheckClassifiesCodexResults(t *testing.T) {
	tests := []struct {
		name         string
		run          CommandResult
		output       string
		wantStatus   string
		wantFailure  string
		wantSignal   string
		wantExitCode int
	}{
		{
			name:         "success",
			run:          CommandResult{ExitCode: 0},
			output:       "ok\n",
			wantStatus:   StatusPassed,
			wantSignal:   "matched",
			wantExitCode: 0,
		},
		{
			name:         "stale_auth",
			run:          CommandResult{ExitCode: 1, Stderr: "not logged in; token expired", Err: errors.New("exit status 1")},
			wantStatus:   StatusFailed,
			wantFailure:  FailureAuthFailed,
			wantSignal:   "missing",
			wantExitCode: 1,
		},
		{
			name:         "missing_binary",
			run:          CommandResult{ExitCode: -1, Err: os.ErrNotExist},
			wantStatus:   StatusFailed,
			wantFailure:  FailureBinaryMissing,
			wantSignal:   "missing",
			wantExitCode: -1,
		},
		{
			name:         "timeout",
			run:          CommandResult{TimedOut: true, Err: context.DeadlineExceeded},
			wantStatus:   StatusFailed,
			wantFailure:  FailureTimeout,
			wantSignal:   "missing",
			wantExitCode: 0,
		},
		{
			name:         "provider_unavailable",
			run:          CommandResult{ExitCode: 1, Stderr: "service unavailable: 503", Err: errors.New("exit status 1")},
			wantStatus:   StatusFailed,
			wantFailure:  FailureUnavailable,
			wantSignal:   "missing",
			wantExitCode: 1,
		},
		{
			name:         "successful_exit_with_output_drift",
			run:          CommandResult{ExitCode: 0, Stdout: "{}\n", Stderr: "info\n"},
			output:       "hello",
			wantStatus:   StatusPassed,
			wantSignal:   "mismatch",
			wantExitCode: 0,
		},
		{
			name:         "successful_exit_with_missing_signal_file",
			run:          CommandResult{ExitCode: 0},
			wantStatus:   StatusPassed,
			wantSignal:   "missing",
			wantExitCode: 0,
		},
	}
	// #556: this suite exercises the LIVE-smoke classifier (classifyCodexResult)
	// end-to-end through Check, so bypass the offline auth.json short-circuit that
	// would otherwise resolve first. The offline path has its own dedicated tests.
	defer withoutOfflineAuthProbe()()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Check(context.Background(), Params{
				Provider: ProviderCodex,
				RunID:    "run_1",
				LaneID:   "author",
				Env:      []string{"HOME=/home/lane", "PATH=/usr/bin"},
				Runner: RunnerFunc(func(_ context.Context, spec CommandSpec) CommandResult {
					if tt.output != "" {
						if err := os.WriteFile(outputPathFromSpec(t, spec), []byte(tt.output), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return tt.run
				}),
			})
			if result.Status != tt.wantStatus || result.FailureClass != tt.wantFailure {
				t.Fatalf("result = %#v, want status=%s failure=%s", result, tt.wantStatus, tt.wantFailure)
			}
			if result.SuccessSignal != tt.wantSignal || result.ExitCode != tt.wantExitCode {
				t.Fatalf("diagnostics = signal=%q exit=%d, want signal=%q exit=%d", result.SuccessSignal, result.ExitCode, tt.wantSignal, tt.wantExitCode)
			}
			payload, err := json.Marshal(result.ToMap())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(payload), "not logged in") || strings.Contains(string(payload), "token expired") || strings.Contains(string(payload), "hello") {
				t.Fatalf("safe result leaked raw provider output: %s", string(payload))
			}
			if result.ToMap()["probe"] != "codex_exec_output_last_message" {
				t.Fatalf("probe = %#v", result.ToMap()["probe"])
			}
			if result.ToMap()["success_signal"] != tt.wantSignal {
				t.Fatalf("success_signal = %#v, want %q", result.ToMap()["success_signal"], tt.wantSignal)
			}
			if result.ToMap()["raw_output_returned"] != false {
				t.Fatalf("raw_output_returned = %#v", result.ToMap()["raw_output_returned"])
			}
		})
	}
}

func TestUnsupportedProviderResultIsSafe(t *testing.T) {
	result := Check(context.Background(), Params{Provider: "agy"})
	if result.Status != StatusFailed || result.FailureClass != FailureUnsupported || result.Checked {
		t.Fatalf("unsupported result = %#v", result)
	}
}

// withoutOfflineAuthProbe swaps the offline auth probe for a no-op ("not
// attempted") so a test can drive the live-smoke path (classifyCodexResult /
// the serialization lock) deterministically. It returns a restore func.
func withoutOfflineAuthProbe() func() {
	prev := offlineAuthProbe
	offlineAuthProbe = func(context.Context, Runner, Params) codexOfflineAuthOutcome {
		return codexOfflineAuthOutcome{}
	}
	return func() { offlineAuthProbe = prev }
}

func TestSerializationIsPerProviderAuthHome(t *testing.T) {
	defer withoutOfflineAuthProbe()()
	var inFlight int32
	var maxInFlight int32
	runner := RunnerFunc(func(_ context.Context, spec CommandSpec) CommandResult {
		current := atomic.AddInt32(&inFlight, 1)
		for {
			max := atomic.LoadInt32(&maxInFlight)
			if current <= max || atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		if err := os.WriteFile(outputPathFromSpec(t, spec), []byte("ok"), 0o600); err != nil {
			t.Fatal(err)
		}
		return CommandResult{ExitCode: 0}
	})

	done := make(chan Result, 2)
	params := Params{
		Provider: ProviderCodex,
		Env:      []string{"HOME=/home/lane", "PATH=/usr/bin"},
		Runner:   runner,
	}
	go func() { done <- Check(context.Background(), params) }()
	go func() { done <- Check(context.Background(), params) }()
	for i := 0; i < 2; i++ {
		if result := <-done; !result.Passed() {
			t.Fatalf("check failed: %#v", result)
		}
	}
	if atomic.LoadInt32(&maxInFlight) != 1 {
		t.Fatalf("checks for same provider auth home ran concurrently: max=%d", maxInFlight)
	}
}

// TestNonAuthNonzeroExitIsNotAuthFailure is the #556 Defect-A regression: a
// nonzero codex exit that does NOT name an auth problem (a read-only-sandbox
// network block, quota, or a flag/version drift) must classify as UNAVAILABLE,
// not lane_provider_auth_failed. Mislabeling these refused every codex lane
// while auth was actually valid.
func TestNonAuthNonzeroExitIsNotAuthFailure(t *testing.T) {
	cases := []struct {
		name   string
		run    CommandResult
		expect string
	}{
		{
			name:   "read_only_sandbox_network_block",
			run:    CommandResult{ExitCode: 1, Stderr: "error sending request: connection refused (os error 111)", Err: errors.New("exit status 1")},
			expect: FailureUnavailable,
		},
		{
			name:   "unrecognized_flag_drift",
			run:    CommandResult{ExitCode: 2, Stderr: "error: unexpected argument '--ignore-rules' found", Err: errors.New("exit status 2")},
			expect: FailureUnavailable,
		},
		{
			name:   "bare_nonzero_no_signal",
			run:    CommandResult{ExitCode: 1, Stderr: "panic: something went wrong", Err: errors.New("exit status 1")},
			expect: FailureUnavailable,
		},
		{
			name:   "genuine_auth_still_detected",
			run:    CommandResult{ExitCode: 1, Stderr: "not logged in; run `codex login`", Err: errors.New("exit status 1")},
			expect: FailureAuthFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyCodexResult(Result{Status: StatusPassed}, tc.run, "", errors.New("missing"))
			if got.FailureClass != tc.expect {
				t.Fatalf("FailureClass = %q, want %q (exit=%d stderr=%q)", got.FailureClass, tc.expect, tc.run.ExitCode, tc.run.Stderr)
			}
		})
	}
}

// TestOfflineCodexAuthCheck is the #556 Defect-A offline-probe contract: a valid
// auth.json present in $CODEX_HOME passes WITHOUT any billed model round-trip,
// and an absent or rotten auth.json fails (the gate stays non-vacuous).
func TestOfflineCodexAuthCheck(t *testing.T) {
	t.Run("valid_oauth_tokens_passes_offline", func(t *testing.T) {
		home := t.TempDir()
		codexHome := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, CodexAuthFileName),
			[]byte(`{"tokens":{"access_token":"ya29.real","id_token":"id","refresh_token":"rt"},"OPENAI_API_KEY":null}`), 0o600); err != nil {
			t.Fatal(err)
		}
		smokeRan := false
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=" + home, "PATH=/usr/bin"},
			Runner: RunnerFunc(func(context.Context, CommandSpec) CommandResult {
				smokeRan = true
				return CommandResult{ExitCode: 1, Stderr: "should not run"}
			}),
		})
		if !result.Passed() {
			t.Fatalf("offline-valid auth did not pass: %#v", result)
		}
		if smokeRan {
			t.Fatalf("offline-valid auth should skip the billed smoke; smoke ran")
		}
		if result.Probe != ProbeCodexOfflineAuthFile {
			t.Fatalf("probe = %q, want %q", result.Probe, ProbeCodexOfflineAuthFile)
		}
		if result.Costing != "no_provider_tokens_spent" {
			t.Fatalf("costing = %q, want no_provider_tokens_spent", result.Costing)
		}
	})

	t.Run("flat_api_key_passes_offline", func(t *testing.T) {
		home := t.TempDir()
		codexHome := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, CodexAuthFileName),
			[]byte(`{"OPENAI_API_KEY":"sk-live-xyz"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=" + home, "PATH=/usr/bin"},
			Runner: RunnerFunc(func(context.Context, CommandSpec) CommandResult {
				t.Fatalf("billed smoke must not run when offline auth is valid")
				return CommandResult{}
			}),
		})
		if !result.Passed() {
			t.Fatalf("flat-key offline auth did not pass: %#v", result)
		}
	})

	t.Run("absent_auth_fails_as_auth_gap", func(t *testing.T) {
		home := t.TempDir() // no .codex/auth.json
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=" + home, "PATH=/usr/bin"},
			Runner: RunnerFunc(func(context.Context, CommandSpec) CommandResult {
				t.Fatalf("billed smoke must not run when auth.json is definitively absent")
				return CommandResult{}
			}),
		})
		if result.Passed() || result.FailureClass != FailureAuthFailed {
			t.Fatalf("absent auth.json must fail as auth gap: %#v", result)
		}
	})

	t.Run("rotten_json_fails_as_auth_gap", func(t *testing.T) {
		home := t.TempDir()
		codexHome := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, CodexAuthFileName), []byte("not json at all"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=" + home, "PATH=/usr/bin"},
			Runner: RunnerFunc(func(context.Context, CommandSpec) CommandResult {
				t.Fatalf("billed smoke must not run for a present-but-corrupt auth.json")
				return CommandResult{}
			}),
		})
		if result.Passed() || result.FailureClass != FailureAuthFailed {
			t.Fatalf("corrupt auth.json must fail as auth gap: %#v", result)
		}
	})

	t.Run("fieldless_json_fails_as_auth_gap", func(t *testing.T) {
		home := t.TempDir()
		codexHome := filepath.Join(home, ".codex")
		if err := os.MkdirAll(codexHome, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, CodexAuthFileName), []byte(`{"unrelated":"value"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=" + home, "PATH=/usr/bin"},
			Runner:   RunnerFunc(func(context.Context, CommandSpec) CommandResult { return CommandResult{} }),
		})
		if result.Passed() || result.FailureClass != FailureAuthFailed {
			t.Fatalf("credential-less auth.json must fail as auth gap: %#v", result)
		}
	})

	t.Run("codex_home_override_respected", func(t *testing.T) {
		codexHome := t.TempDir()
		if err := os.WriteFile(filepath.Join(codexHome, CodexAuthFileName), []byte(`{"OPENAI_API_KEY":"sk-x"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		result := Check(context.Background(), Params{
			Provider: ProviderCodex,
			Env:      []string{"HOME=/home/nope", "CODEX_HOME=" + codexHome, "PATH=/usr/bin"},
			Runner:   RunnerFunc(func(context.Context, CommandSpec) CommandResult { return CommandResult{} }),
		})
		if !result.Passed() {
			t.Fatalf("CODEX_HOME-pinned auth.json did not pass: %#v", result)
		}
	})
}

func outputPathFromSpec(t *testing.T, spec CommandSpec) string {
	t.Helper()
	args := append([]string{spec.Name}, spec.Args...)
	for i, arg := range args {
		if arg == "--output-last-message" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("spec missing --output-last-message: %#v", spec)
	return filepath.Join(t.TempDir(), "missing")
}
