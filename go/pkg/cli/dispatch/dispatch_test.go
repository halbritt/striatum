package dispatch

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
)

type fakeInvoker struct {
	calls []call
	err   error
}

type call struct {
	method string
	params map[string]any
}

func (f *fakeInvoker) Invoke(_ context.Context, method string, params map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, call{method: method, params: params})
	if f.err != nil {
		return nil, f.err
	}
	if method == "repo.resolve" {
		return map[string]any{"repository_id": "repo_resolved"}, nil
	}
	if method == "supervise.trajectory" {
		return map[string]any{
			"content": "booted\nready\n",
			"trajectory_log": map[string]any{
				"status": "available",
			},
		}, nil
	}
	return map[string]any{"method": method}, nil
}

// TestDispatchPrintsRemediationSuggestion is the #358 item-4 regression: the RFC
// 0111 error catalog fills rpc.Error.Suggestion on the daemon side and rpcError
// threads it onto rpcclient.Error; writeError must surface it so the agent-driven
// CLI consumer sees the exact remedy, not just the failure message.
func TestDispatchPrintsRemediationSuggestion(t *testing.T) {
	invoker := &fakeInvoker{err: &rpcclient.Error{
		Code:       "blob_apply_required",
		Message:    "bucket does not exist",
		Suggestion: "Re-run with apply enabled to create the bucket.",
		ExitCode:   1,
	}}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repository-id", "repo_1", "run", "start", "run_1"},
		&stdout, &stderr, Options{Invoker: invoker, ExitCode: rpcclient.ExitCode})
	if exit == 0 {
		t.Fatalf("exit = %d, want non-zero on a daemon error", exit)
	}
	out := stderr.String()
	if !strings.Contains(out, "bucket does not exist") {
		t.Fatalf("stderr missing failure message: %q", out)
	}
	if !strings.Contains(out, "suggestion: Re-run with apply enabled to create the bucket.") {
		t.Fatalf("stderr missing RFC 0111 remediation suggestion: %q", out)
	}
}

// TestDispatchNoSuggestionLineWhenAbsent guards against a stray "suggestion:"
// line when the daemon error carries no remediation.
func TestDispatchNoSuggestionLineWhenAbsent(t *testing.T) {
	invoker := &fakeInvoker{err: &rpcclient.Error{Code: "internal_error", Message: "boom", ExitCode: 1}}
	var stdout, stderr bytes.Buffer
	_ = Run(context.Background(), []string{"--repository-id", "repo_1", "run", "start", "run_1"},
		&stdout, &stderr, Options{Invoker: invoker, ExitCode: rpcclient.ExitCode})
	if strings.Contains(stderr.String(), "suggestion:") {
		t.Fatalf("stderr printed a suggestion line for a suggestion-less error: %q", stderr.String())
	}
}

func TestDispatchRoutesReadThroughRPC(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repo", "/repo", "status", "--run-id", "run_1"}, &stdout, &stderr, Options{
		Invoker:     invoker,
		ResolveRepo: true,
		Cwd:         "/cwd",
		Env:         []string{},
	})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 2 {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	if invoker.calls[0].method != "repo.resolve" || invoker.calls[0].params["path"] != "/repo" {
		t.Fatalf("resolve call = %#v", invoker.calls[0])
	}
	if invoker.calls[1].method != "status" || invoker.calls[1].params["repository_id"] != "repo_resolved" || invoker.calls[1].params["run_id"] != "run_1" {
		t.Fatalf("status call = %#v", invoker.calls[1])
	}
}

func TestDispatchResolvesRelativeRepoPathBeforeDaemonRPC(t *testing.T) {
	cwd := t.TempDir()
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repo", ".", "status"}, &stdout, &stderr, Options{
		Invoker:     invoker,
		ResolveRepo: true,
		Cwd:         cwd,
		Env:         []string{},
	})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if got := invoker.calls[0].params["path"]; got != filepath.Clean(cwd) {
		t.Fatalf("repo.resolve path = %#v, want client cwd %q", got, filepath.Clean(cwd))
	}
}

func TestDispatchResolvesNestedRelativeRepoPathBeforeDaemonRPC(t *testing.T) {
	cwd := t.TempDir()
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repo", "child/repo", "status"}, &stdout, &stderr, Options{
		Invoker:     invoker,
		ResolveRepo: true,
		Cwd:         cwd,
		Env:         []string{},
	})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	want := filepath.Join(cwd, "child", "repo")
	if got := invoker.calls[0].params["path"]; got != want {
		t.Fatalf("repo.resolve path = %#v, want %q", got, want)
	}
}

func TestDispatchRoutesMutationThroughRPC(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repository-id", "repo_1", "run", "start", "run_1"}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if invoker.calls[0].method != "run.start" || invoker.calls[0].params["run_id"] != "run_1" {
		t.Fatalf("call = %#v", invoker.calls[0])
	}
}

// #182: `daemon token-create` routes to the daemon.token.create RPC so an
// operator can mint an apply-capable token (run.integrate) without raw RPC.
// It is daemon_global, so no repository_id resolution happens, and repeated
// --capability flags collapse into a capabilities array.
func TestDispatchRoutesDaemonTokenCreate(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"daemon", "token-create",
		"--capability", "apply", "--capability", "admin",
		"--display-name", "operator-apply"}, &stdout, &stderr, Options{Invoker: invoker, ResolveRepo: true, Env: []string{}})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 {
		t.Fatalf("expected a single RPC call (no repo.resolve for daemon_global), got %#v", invoker.calls)
	}
	got := invoker.calls[0]
	if got.method != "daemon.token.create" {
		t.Fatalf("method = %q", got.method)
	}
	if _, ok := got.params["repository_id"]; ok {
		t.Fatalf("daemon_global route must not carry repository_id: %#v", got.params)
	}
	caps, ok := got.params["capabilities"].([]any)
	if !ok || len(caps) != 2 || caps[0] != "apply" || caps[1] != "admin" {
		t.Fatalf("capabilities = %#v (params=%#v)", got.params["capabilities"], got.params)
	}
	if got.params["display_name"] != "operator-apply" {
		t.Fatalf("display_name = %#v", got.params["display_name"])
	}
}

// #276: a supervised lane carries STRIATUM_REPOSITORY_ID in its environment. A
// daemon_global route must not pick that ambient value up and attach it as a
// repository_id param — doing so leaks lane runtime state into a daemon-global
// RPC and (because dispatch falls back to os.Environ() when Options.Env is nil)
// breaks `make check` when it runs inside a lane. This pins the gate that keeps
// the ambient env out of daemon_global params while leaving explicit
// --repository-id untouched.
func TestDispatchDaemonGlobalIgnoresAmbientRepositoryID(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"daemon", "token-create",
		"--capability", "apply", "--display-name", "operator-apply"},
		&stdout, &stderr, Options{
			Invoker:     invoker,
			ResolveRepo: true,
			Env:         []string{"STRIATUM_REPOSITORY_ID=repo_ambient_lane"},
		})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 {
		t.Fatalf("expected a single RPC call (no repo.resolve for daemon_global), got %#v", invoker.calls)
	}
	got := invoker.calls[0]
	if got.method != "daemon.token.create" {
		t.Fatalf("method = %q", got.method)
	}
	if v, ok := got.params["repository_id"]; ok {
		t.Fatalf("daemon_global route leaked ambient repository_id %#v into params: %#v", v, got.params)
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"bogus"}, &stdout, &stderr, Options{Invoker: &fakeInvoker{}})
	if exit != 2 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestSuggestCommandRescuesTranspositionAndPlural(t *testing.T) {
	cases := map[string]string{
		"run list":      "list runs",     // transposition + plural
		"runs list":     "list runs",     // plural on first token
		"sessions list": "list sessions", // transposition + plural
		"list run":      "list runs",     // singular subcommand
	}
	for input, want := range cases {
		if got := suggestCommand(strings.Fields(input)); got != want {
			t.Fatalf("suggestCommand(%q) = %q, want %q", input, got, want)
		}
	}
	if got := suggestCommand([]string{"totally", "bogus"}); got != "" {
		t.Fatalf("expected no suggestion for nonsense, got %q", got)
	}
}

func TestDispatchUnknownCommandSuggests(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"run", "list"}, &stdout, &stderr, Options{Invoker: &fakeInvoker{}})
	if exit != 2 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stderr.String(), "did you mean: striatum list runs") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestDispatchHelpListsRequiredAndOptionalFlags(t *testing.T) {
	cases := map[string][]string{
		"supervise stop":       {"--session-id", "--reason", "required:"},
		"register-session":     {"--capability", "--fresh", "run-id", "role", "lane"},
		"doctor":               {"--lane-provider-auth", "codex", "--timeout", "doctor"},
		"checkpoint resolve":   {"continue|cancel", "--decision-id", "blocker-id"},
		"supervise start":      {"--session-id", "--provider-auth-gate", "auto|required|off", "supervise.start"},
		"supervise send":       {"--packet-id", "supervise.send"},
		"supervise status":     {"--session-id", "supervise.status"},
		"supervise trajectory": {"--session-id", "--tail", "--tail-lines", "supervise.trajectory"},
		"repo add":             {"--init", "path", "repo.add"},
		"repo write":           {"session-id", "job-id", "lease-id", "--content", "repo.write"},
		"repo patch-preview":   {"session-id", "job-id", "lease-id", "--patch", "repo.patch_preview"},
		"process run":          {"session-id", "job-id", "lease-id", "--command-json", "process.run"},
		"decision record":      {"run-id", "outcome", "--escape-surface", "--mark-run-compromised", "decision.record"},
	}
	for cmd, wants := range cases {
		invoker := &fakeInvoker{}
		var stdout, stderr bytes.Buffer
		args := append(strings.Fields(cmd), "--help")
		exit := Run(context.Background(), args, &stdout, &stderr, Options{Invoker: invoker})
		if exit != 0 {
			t.Fatalf("%s --help exit = %d stderr=%s", cmd, exit, stderr.String())
		}
		if len(invoker.calls) != 0 {
			t.Fatalf("%s --help contacted the daemon: %#v", cmd, invoker.calls)
		}
		out := stdout.String()
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Fatalf("%s --help output missing %q:\n%s", cmd, want, out)
			}
		}
	}
}

// TestDispatchCommandGroupHelpListsRecoverySubcommands (#389 gap 3): `recovery
// --help` (and bare `recovery`) must list the recovery verb family without
// contacting the daemon, instead of erroring "unknown command: recovery".
func TestDispatchCommandGroupHelpListsRecoverySubcommands(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		invoker := &fakeInvoker{}
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"recovery", "--help"}, &stdout, &stderr, Options{Invoker: invoker})
		if exit != 0 {
			t.Fatalf("recovery --help exit = %d stderr=%s", exit, stderr.String())
		}
		if len(invoker.calls) != 0 {
			t.Fatalf("recovery --help contacted the daemon: %#v", invoker.calls)
		}
		out := stdout.String()
		for _, want := range []string{"reseal", "resolve-blocker", "complete-stalled", "requeue-stale"} {
			if !strings.Contains(out, want) {
				t.Fatalf("recovery --help output missing %q:\n%s", want, out)
			}
		}
	})
	t.Run("bare", func(t *testing.T) {
		invoker := &fakeInvoker{}
		var stdout, stderr bytes.Buffer
		exit := Run(context.Background(), []string{"recovery"}, &stdout, &stderr, Options{Invoker: invoker})
		if exit != 2 {
			t.Fatalf("bare recovery exit = %d, want 2 (usage error); stdout=%s", exit, stdout.String())
		}
		if len(invoker.calls) != 0 {
			t.Fatalf("bare recovery contacted the daemon: %#v", invoker.calls)
		}
		if !strings.Contains(stderr.String(), "reseal") {
			t.Fatalf("bare recovery stderr missing subcommand list:\n%s", stderr.String())
		}
	})
}

func TestDispatchDoctorLaneProviderAuthParams(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{
		"--repository-id", "repo_1",
		"doctor",
		"--lane-provider-auth", "codex",
		"--run-id", "run_1",
		"--lane-id", "author",
		"--timeout", "12s",
	}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "doctor" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["lane_provider_auth"] != "codex" || params["run_id"] != "run_1" || params["lane_id"] != "author" || params["timeout"] != "12s" {
		t.Fatalf("doctor params = %#v", params)
	}
}

func TestDispatchSuperviseStartProviderAuthGateParam(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{
		"--repository-id", "repo_1",
		"supervise", "start", "sess_1",
		"--provider-auth-gate", "required",
	}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "supervise.start" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["session_id"] != "sess_1" || params["provider_auth_gate"] != "required" {
		t.Fatalf("supervise.start params = %#v", params)
	}
}

func TestDispatchDecisionRecordPassesCompromiseMarker(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{
		"--repository-id", "repo_1",
		"decision", "record",
		"run_1", "docs/decisions/compromised.md", "accepted", "Invalidate compromised provenance",
		"--rationale", "Review provenance was compromised; replacement run required.",
		"--mark-run-compromised",
	}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "decision.record" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["repository_id"] != "repo_1" ||
		params["run_id"] != "run_1" ||
		params["path"] != "docs/decisions/compromised.md" ||
		params["outcome"] != "accepted" ||
		params["title"] != "Invalidate compromised provenance" ||
		params["mark_run_compromised"] != true {
		t.Fatalf("params = %#v", params)
	}
}

func TestDispatchSuperviseTrajectoryPrintsContent(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repository-id", "repo_1", "supervise", "trajectory", "sess_1", "--tail-lines", "2"}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if stdout.String() != "booted\nready\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "supervise.trajectory" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["session_id"] != "sess_1" || params["tail_lines"] != 2 {
		t.Fatalf("params = %#v", params)
	}
}

func TestDispatchSuperviseTrajectoryTailNumericAlias(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repository-id", "repo_1", "supervise", "trajectory", "sess_1", "--tail", "120"}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "supervise.trajectory" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["tail"] != nil || params["tail_lines"] != 120 {
		t.Fatalf("params = %#v", params)
	}
}

func TestDispatchSessionRegisterAliasResolvesToSessionRegister(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repository-id", "repo_1", "session", "register", "run_1", "author", "lane_a"}, &stdout, &stderr, Options{Invoker: invoker})
	if exit != 0 {
		t.Fatalf("exit = %d stderr=%s", exit, stderr.String())
	}
	if len(invoker.calls) != 1 || invoker.calls[0].method != "session.register" {
		t.Fatalf("calls = %#v", invoker.calls)
	}
	params := invoker.calls[0].params
	if params["run_id"] != "run_1" || params["role"] != "author" || params["lane"] != "lane_a" {
		t.Fatalf("params = %#v", params)
	}
}

func TestDispatchUsesExitCodeMapper(t *testing.T) {
	wantErr := errors.New("daemon down")
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"repo", "list"}, &stdout, &stderr, Options{
		Invoker:  &fakeInvoker{err: wantErr},
		ExitCode: func(err error) int { return 11 },
	})
	if exit != 11 {
		t.Fatalf("exit = %d", exit)
	}
}
