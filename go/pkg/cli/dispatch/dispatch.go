package dispatch

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
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/params"
	"github.com/halbritt/striatum/go/pkg/cli/routes"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
)

type Invoker interface {
	Invoke(context.Context, string, map[string]any) (map[string]any, error)
}

type Options struct {
	Env              []string
	Cwd              string
	Invoker          Invoker
	InvokerFactory   func(RuntimeConfig) (Invoker, error)
	ResolveRepo      bool
	ResolveRepoRoute string
	ExitCode         func(error) int
}

type RuntimeConfig struct {
	SocketPath string
	Token      string
	TokenFile  string
	DeadlineMS int
}

type globalOptions struct {
	RepoPath     string
	RepositoryID string
	SocketPath   string
	Token        string
	TokenFile    string
	DeadlineMS   int
	JSONOutput   bool
	CommandArgs  []string
}

// suggestCommand offers a "did you mean" for an unknown command by matching the
// typed leading tokens (order-independent, singular/plural-insensitive) against
// the known route table. It rescues common transpositions like `run list` ->
// `list runs` without hand-maintaining aliases (#48).
func suggestCommand(args []string) string {
	norm := func(s string) string { return strings.TrimSuffix(strings.ToLower(s), "s") }
	lead := []string{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		lead = append(lead, norm(a))
		if len(lead) == 2 {
			break
		}
	}
	if len(lead) == 0 {
		return ""
	}
	typed := map[string]bool{}
	for _, t := range lead {
		typed[t] = true
	}
	for _, r := range routes.All() {
		toks := map[string]bool{norm(r.Command): true}
		if r.Subcommand != "" {
			toks[norm(r.Subcommand)] = true
		}
		if len(toks) != len(typed) {
			continue
		}
		match := true
		for t := range toks {
			if !typed[t] {
				match = false
				break
			}
		}
		if match {
			if r.Subcommand != "" {
				return r.Command + " " + r.Subcommand
			}
			return r.Command
		}
	}
	return ""
}

// requestsHelp reports whether the post-verb args ask for usage. Help wins
// over any other arg so `striatum supervise stop --help` prints usage instead
// of failing on the missing required --reason.
func requestsHelp(args []string) bool {
	for _, arg := range args {
		if routes.IsHelpArg(arg) {
			return true
		}
	}
	return false
}

func Run(ctx context.Context, args []string, stdout io.Writer, stderr io.Writer, options Options) int {
	globals, err := parseGlobal(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	route, consumed, ok := routes.Lookup(globals.CommandArgs)
	if !ok {
		if len(globals.CommandArgs) > 0 {
			// #389 gap 3: a command group that carries only subcommands (e.g.
			// `recovery`) misses Lookup when typed bare or with --help, and used to
			// fall through to "unknown command: recovery", leaving the recovery verb
			// family undiscoverable. Render the subcommand list instead. `--help`
			// prints to stdout (exit 0); a bare group with no subcommand is a usage
			// error printed to stderr (exit 2).
			command := globals.CommandArgs[0]
			rest := globals.CommandArgs[1:]
			if groupHelp := routes.RenderCommandGroupHelp(command); groupHelp != "" &&
				(len(rest) == 0 || requestsHelp(rest)) {
				if requestsHelp(rest) {
					_, _ = fmt.Fprint(stdout, groupHelp)
					return 0
				}
				_, _ = fmt.Fprint(stderr, groupHelp)
				return 2
			}
			_, _ = fmt.Fprintf(stderr, "unknown command: %s\n", strings.Join(globals.CommandArgs, " "))
			if suggestion := suggestCommand(globals.CommandArgs); suggestion != "" {
				_, _ = fmt.Fprintf(stderr, "did you mean: striatum %s\n", suggestion)
			}
		} else {
			_, _ = fmt.Fprintln(stderr, "usage: striatum [global options] command ...")
		}
		return 2
	}
	// `--help`/`-h`/`help` after the verb prints usage (required + optional
	// flags) without contacting the daemon, so operators discover params from
	// help instead of runtime "<method> requires <param>" errors (issue #63 F9).
	if requestsHelp(globals.CommandArgs[consumed:]) {
		_, _ = fmt.Fprint(stdout, route.RenderHelp())
		return 0
	}
	invoker := options.Invoker
	if invoker == nil && options.InvokerFactory != nil {
		invoker, err = options.InvokerFactory(RuntimeConfig{
			SocketPath: globals.SocketPath,
			Token:      globals.Token,
			TokenFile:  globals.TokenFile,
			DeadlineMS: globals.DeadlineMS,
		})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}
	if invoker == nil {
		_, _ = fmt.Fprintln(stderr, "daemon RPC invoker is not configured")
		return 1
	}
	repositoryID := globals.RepositoryID
	// #276: only the ambient STRIATUM_REPOSITORY_ID is gated on the route being
	// repo-scoped. A daemon_global route has no repository scope, so letting a
	// lane-control env var attach repository_id leaks lane runtime state into a
	// daemon-global RPC (and contaminates daemon_global dispatch tests run from a
	// supervised lane). An explicit --repository-id (globals.RepositoryID) is
	// operator intent and is left unchanged for every route.
	if repositoryID == "" && route.RepositoryScopeMode != "daemon_global" {
		repositoryID = envValue(options.Env, "STRIATUM_REPOSITORY_ID")
	}
	if route.RepositoryScopeMode == "single_repo" && repositoryID == "" {
		if options.ResolveRepo {
			resolved, err := resolveRepository(ctx, invoker, globals.RepoPath, options)
			if err != nil {
				return writeError(stderr, err, options)
			}
			repositoryID = resolved
		}
	}
	params, err := buildParams(route, globals.CommandArgs[consumed:], repositoryID)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	data, err := invoker.Invoke(ctx, route.Method, params)
	if err != nil {
		return writeError(stderr, err, options)
	}
	if route.Method == "trajectory.watch" && !globals.JSONOutput {
		return runWatchLoop(ctx, invoker, route, params, data, stdout, stderr)
	}
	if route.Method == "trajectory.export" && !globals.JSONOutput {
		return writeJSONL(stdout, data, stderr)
	}
	if route.Method == "supervise.trajectory" && !globals.JSONOutput {
		if content, ok := data["content"].(string); ok {
			_, err := fmt.Fprint(stdout, content)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return 1
			}
			if content != "" && !strings.HasSuffix(content, "\n") {
				_, _ = fmt.Fprintln(stdout)
			}
			if content == "" {
				return writeJSON(stdout, data, stderr)
			}
			return 0
		}
	}
	if globals.JSONOutput {
		return writeJSON(stdout, map[string]any{"ok": true, "data": data}, stderr)
	}
	return writeJSON(stdout, data, stderr)
}

func writeJSONL(stdout io.Writer, data map[string]any, stderr io.Writer) int {
	records, ok := data["records"].([]any)
	if !ok {
		return writeJSON(stdout, data, stderr)
	}
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return 1
		}
		_, _ = fmt.Fprintln(stdout, string(encoded))
	}
	return 0
}

func runWatchLoop(ctx context.Context, invoker Invoker, route routes.Route, params map[string]any, initialData map[string]any, stdout io.Writer, stderr io.Writer) int {
	data := initialData
	for {
		records, _ := data["records"].([]any)
		for _, record := range records {
			encoded, _ := json.Marshal(record)
			_, _ = fmt.Fprintln(stdout, string(encoded))
			if m, ok := record.(map[string]any); ok {
				if seq, ok := m["seq"].(float64); ok {
					params["since_seq"] = int64(seq)
				} else if seq, ok := m["seq"].(int64); ok {
					params["since_seq"] = seq
				}
			}
		}

		select {
		case <-ctx.Done():
			return 0
		case <-time.After(1 * time.Second):
		}

		var err error
		data, err = invoker.Invoke(ctx, route.Method, params)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return 1
		}
	}
}

func parseGlobal(args []string) (globalOptions, error) {
	var options globalOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			options.CommandArgs = append(options.CommandArgs, args[i+1:]...)
			return options, nil
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			options.CommandArgs = append(options.CommandArgs, args[i:]...)
			return options, nil
		}
		keyValue := strings.TrimPrefix(arg, "--")
		key, value, hasValue := strings.Cut(keyValue, "=")
		switch key {
		case "repo":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--repo requires a value")
			}
			options.RepoPath = value
		case "repository-id":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--repository-id requires a value")
			}
			options.RepositoryID = value
		case "daemon-socket":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--daemon-socket requires a value")
			}
			options.SocketPath = value
		case "capability-token":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--capability-token requires a value")
			}
			options.Token = value
		case "capability-token-file":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--capability-token-file requires a value")
			}
			options.TokenFile = value
		case "deadline-ms":
			value, hasValue, i = readGlobalValue(args, i, value, hasValue)
			if !hasValue {
				return options, fmt.Errorf("--deadline-ms requires a value")
			}
			deadline, err := strconv.Atoi(value)
			if err != nil || deadline < 0 {
				return options, fmt.Errorf("--deadline-ms must be a non-negative integer")
			}
			options.DeadlineMS = deadline
		case "json":
			options.JSONOutput = true
		default:
			options.CommandArgs = append(options.CommandArgs, args[i:]...)
			return options, nil
		}
	}
	return options, nil
}

func readGlobalValue(args []string, i int, value string, hasValue bool) (string, bool, int) {
	if hasValue {
		return value, true, i
	}
	if i+1 >= len(args) {
		return "", false, i
	}
	return args[i+1], true, i + 1
}

func buildParams(route routes.Route, args []string, repositoryID string) (map[string]any, error) {
	// Both the read and mutation param groups resolve through the same
	// params.Build; the former mutationparams/readparams forwarders were
	// identical aliases (removed in #357).
	return params.Build(route.ParamsGroup, args, params.Options{RepositoryID: repositoryID})
}

func resolveRepository(ctx context.Context, invoker Invoker, repoPath string, options Options) (string, error) {
	resolvedPath, err := clientAbsoluteRepoPath(repoPath, options.Cwd)
	if err != nil {
		return "", err
	}
	route := options.ResolveRepoRoute
	if route == "" {
		route = "repo.resolve"
	}
	data, err := invoker.Invoke(ctx, route, map[string]any{"path": resolvedPath})
	if err != nil {
		return "", err
	}
	repositoryID, _ := data["repository_id"].(string)
	if repositoryID == "" {
		return "", fmt.Errorf("repo.resolve response did not include repository_id")
	}
	return repositoryID, nil
}

func clientAbsoluteRepoPath(repoPath, cwd string) (string, error) {
	if repoPath == "" {
		if cwd != "" {
			repoPath = cwd
		} else {
			var err error
			repoPath, err = os.Getwd()
			if err != nil {
				return "", err
			}
		}
	}
	if filepath.IsAbs(repoPath) {
		return filepath.Clean(repoPath), nil
	}
	if cwd != "" {
		return filepath.Abs(filepath.Join(cwd, repoPath))
	}
	return filepath.Abs(repoPath)
}

func writeError(stderr io.Writer, err error, options Options) int {
	_, _ = fmt.Fprintln(stderr, err.Error())
	// RFC 0111 P2: surface the daemon-supplied remediation so the 72-code error
	// catalog's entire point — naming the exact remedy — reaches the CLI consumer
	// (#358). The suggestion rides on the rpcclient.Error the daemon path returns.
	var clientErr *rpcclient.Error
	if errors.As(err, &clientErr) && strings.TrimSpace(clientErr.Suggestion) != "" {
		_, _ = fmt.Fprintf(stderr, "suggestion: %s\n", clientErr.Suggestion)
	}
	if options.ExitCode != nil {
		return options.ExitCode(err)
	}
	return 1
}

func writeJSON(stdout io.Writer, payload any, stderr io.Writer) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	_, _ = fmt.Fprintln(stdout, string(encoded))
	return 0
}

func envValue(env []string, key string) string {
	if env == nil {
		env = os.Environ()
	}
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}
