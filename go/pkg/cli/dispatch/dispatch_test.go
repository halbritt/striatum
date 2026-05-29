package dispatch

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
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
	return map[string]any{"method": method}, nil
}

func TestDispatchRoutesReadThroughRPC(t *testing.T) {
	invoker := &fakeInvoker{}
	var stdout, stderr bytes.Buffer
	exit := Run(context.Background(), []string{"--repo", "/repo", "status", "--run-id", "run_1"}, &stdout, &stderr, Options{
		Invoker:     invoker,
		ResolveRepo: true,
		Cwd:         "/cwd",
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
