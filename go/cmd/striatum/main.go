package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/dispatch"
	"github.com/halbritt/striatum/go/pkg/cli/localcommands"
	"github.com/halbritt/striatum/go/pkg/cli/routes"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	"github.com/halbritt/striatum/go/pkg/cli/rundrive"
	cliskills "github.com/halbritt/striatum/go/pkg/cli/skills"
	"github.com/halbritt/striatum/go/pkg/laneproviderauth"
	"github.com/halbritt/striatum/go/pkg/verifier"
	"github.com/halbritt/striatum/go/pkg/workflowauthoring"
	"github.com/halbritt/striatum/go/pkg/workflowgenerate"
	"github.com/halbritt/striatum/go/pkg/workflowtemplates"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	globals, err := parseLeadingGlobals(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	// scope-check is a single-token local diagnostic (no daemon dependency, no
	// mutation): a pre-work.complete write_scope drift check (issue #91 /
	// RFC 0099 Phase-1 seed). Dispatch it before the daemon route so it never
	// requires an endpoint or capability token.
	if len(globals.CommandArgs) > 0 && globals.CommandArgs[0] == "scope-check" {
		scopeArgs := globals.CommandArgs[1:]
		if globals.JSONOutput && !containsFlag(scopeArgs, "--json") {
			scopeArgs = append(scopeArgs, "--json")
		}
		return runScopeCheck(scopeArgs, stdout, stderr, globals.RepoPath)
	}
	// `striatum codex` is a local launcher that wires codex to the live daemon
	// MCP endpoint + runtime client-token so an operator outside a supervised
	// lane no longer has to export STRIATUM_MCP_TOKEN and hand-edit
	// ~/.codex/config.toml on every key rotation (#64). It execs codex, so it
	// must run before the daemon route; all trailing args pass through unchanged.
	if len(globals.CommandArgs) > 0 && globals.CommandArgs[0] == "codex" {
		return runCodex(globals.CommandArgs[1:], stdout, stderr, globals.RepoPath)
	}
	// `striatum verifier run` is the LANE-SIDE entrypoint of the RFC 0134 / D227
	// executable verification half: it runs a content-addressed, allowlisted
	// check under a strict sandbox and mints a receipt. It performs command
	// execution OFF the daemon's gate path (inside the disposable verifier lane),
	// touches no daemon RPC state, and so dispatches as a local command before
	// the daemon route — like scope-check and codex.
	if len(globals.CommandArgs) > 0 && globals.CommandArgs[0] == "verifier" {
		verifierArgs := globals.CommandArgs[1:]
		if globals.JSONOutput && !containsFlag(verifierArgs, "--json") {
			verifierArgs = append(verifierArgs, "--json")
		}
		return runVerifier(verifierArgs, stdout, stderr, globals.RepoPath)
	}
	if len(globals.CommandArgs) > 1 && globals.CommandArgs[0] == "run" && globals.CommandArgs[1] == "drive" {
		driveArgs := append([]string(nil), globals.CommandArgs[2:]...)
		if globals.JSONOutput && !containsFlag(driveArgs, "--json") {
			driveArgs = append(driveArgs, "--json")
		}
		return runRunDrive(driveArgs, stdout, stderr, globals)
	}
	// `run start` runs the start verbatim through the daemon route, then auto-drives
	// the started run in a detached driver so the run reconciles to terminal with no
	// operator (or operator-model) in the loop (#212). Opt out with `--no-drive` or
	// STRIATUM_RUN_DRIVE_AUTO=0.
	if len(globals.CommandArgs) > 1 && globals.CommandArgs[0] == "run" && globals.CommandArgs[1] == "start" {
		return runRunStart(args, stdout, stderr, globals)
	}
	if len(globals.CommandArgs) > 0 && globals.CommandArgs[0] == "operator" {
		operatorArgs := append([]string(nil), globals.CommandArgs[1:]...)
		if globals.JSONOutput && !containsFlag(operatorArgs, "--json") {
			operatorArgs = append(operatorArgs, "--json")
		}
		return runOperator(operatorArgs, stdout, stderr, globals)
	}
	// #111: `workflow` (bare) and `workflow --help` carry no recognized subcommand,
	// so localcommands.Lookup misses them and they fall to the daemon route as an
	// "unknown command". Route them to the local workflow dispatcher (which lists
	// the subcommands) before the lookup.
	if len(globals.CommandArgs) > 0 && globals.CommandArgs[0] == "workflow" &&
		(len(globals.CommandArgs) < 2 || routes.IsHelpArg(globals.CommandArgs[1])) {
		return runWorkflow(globals.CommandArgs[1:], stdout, stderr, globals.RepoPath)
	}
	if len(globals.CommandArgs) > 0 && (globals.CommandArgs[0] == "skills" || globals.CommandArgs[0] == "plugin") &&
		(len(globals.CommandArgs) < 2 || routes.IsHelpArg(globals.CommandArgs[1])) {
		return cliskills.Run(globals.CommandArgs, stdout, stderr, globals.RepoPath, version)
	}
	if _, ok := localcommands.Lookup(globals.CommandArgs); ok {
		commandArgs := globals.CommandArgs
		switch commandArgs[0] {
		case "skills", "plugin":
			runArgs := append([]string(nil), commandArgs...)
			if globals.JSONOutput && !containsFlag(runArgs, "--json") {
				runArgs = append(runArgs, "--json")
			}
			return cliskills.Run(runArgs, stdout, stderr, globals.RepoPath, version)
		case "daemon":
			daemonArgs := append([]string(nil), commandArgs[1:]...)
			if globals.JSONOutput && !containsFlag(daemonArgs, "--json") {
				daemonArgs = append(daemonArgs, "--json")
			}
			return localcommands.RunDaemon(daemonArgs, stdout, stderr, version)
		default:
			workflowArgs := commandArgs[1:]
			if globals.JSONOutput && !containsFlag(workflowArgs, "--json") {
				workflowArgs = append([]string{workflowArgs[0], "--json"}, workflowArgs[1:]...)
			}
			return runWorkflow(workflowArgs, stdout, stderr, globals.RepoPath)
		}
	}
	switch args[0] {
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	case "--version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	default:
		return runDaemonRoute(args, stdout, stderr)
	}
}

// usage lists the available commands so a self-driving lane (or an operator) can
// discover the control surface instead of falling back to raw MCP `tools/list`
// over curl (#104). It enumerates the daemon-routed verbs plus the local
// commands, and names the work-packet loop self-driving lanes run.
func usage(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum [--version] [--repo path|--repository-id id] <command> [subcommand] [flags]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Self-driving lanes run the work-packet loop (or the equivalent MCP tools — e.g.")
	_, _ = fmt.Fprintln(out, "work.await_packet, which has no CLI verb): claim-next -> ack -> do the work ->")
	_, _ = fmt.Fprintln(out, "publish-artifact / submit-review -> complete, then repeat. Add --json for machine output.")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Commands (run `striatum <command> [subcommand] --help` for a command's flags):")

	subs := map[string]map[string]bool{}
	for _, route := range routes.All() {
		if route.Deprecated {
			continue
		}
		if subs[route.Command] == nil {
			subs[route.Command] = map[string]bool{}
		}
		if route.Subcommand != "" {
			subs[route.Command][route.Subcommand] = true
		}
	}
	// Local commands handled before the daemon route (main.go run()).
	for _, local := range []string{"scope-check", "codex", "verifier", "daemon", "skills", "plugin"} {
		if subs[local] == nil {
			subs[local] = map[string]bool{}
		}
	}
	if subs["operator"] == nil {
		subs["operator"] = map[string]bool{}
	}
	subs["operator"]["bootstrap"] = true
	if subs["run"] == nil {
		subs["run"] = map[string]bool{}
	}
	subs["run"]["drive"] = true
	// #122: the daemon-routed workflow subcommands (accept-risk, accepted-risks)
	// appear in routes.All() but the local authoring subcommands (validate,
	// generate, templates) are dispatched in runWorkflow() before the daemon
	// route, so they never appear in routes.All(). Add them here so
	// `striatum --help` lists the full workflow surface.
	for _, authSub := range []string{"validate", "generate", "templates"} {
		if subs["workflow"] == nil {
			subs["workflow"] = map[string]bool{}
		}
		subs["workflow"][authSub] = true
	}
	commands := make([]string, 0, len(subs))
	for name := range subs {
		commands = append(commands, name)
	}
	sort.Strings(commands)
	for _, name := range commands {
		subNames := make([]string, 0, len(subs[name]))
		for sub := range subs[name] {
			subNames = append(subNames, sub)
		}
		sort.Strings(subNames)
		if len(subNames) == 0 {
			_, _ = fmt.Fprintf(out, "  %s\n", name)
		} else {
			_, _ = fmt.Fprintf(out, "  %-18s %s\n", name, strings.Join(subNames, " | "))
		}
	}
}

func runDaemonRoute(args []string, stdout io.Writer, stderr io.Writer) int {
	return dispatch.Run(context.Background(), args, stdout, stderr, dispatch.Options{
		Env:         os.Environ(),
		ResolveRepo: true,
		ExitCode:    rpcclient.ExitCode,
		InvokerFactory: func(runtime dispatch.RuntimeConfig) (dispatch.Invoker, error) {
			config, err := rpcclient.ResolveConfig(os.Environ(), runtime.SocketPath, runtime.Token, runtime.TokenFile, runtime.DeadlineMS)
			if err != nil {
				return nil, err
			}
			return rpcclient.Client{Config: config}, nil
		},
	})
}

type leadingGlobals struct {
	CommandArgs  []string
	RepoPath     string
	RepositoryID string
	SocketPath   string
	Token        string
	TokenFile    string
	DeadlineMS   int
	JSONOutput   bool
}

func parseLeadingGlobals(args []string) (leadingGlobals, error) {
	var globals leadingGlobals
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			globals.CommandArgs = args[i+1:]
			return globals, nil
		}
		if !strings.HasPrefix(arg, "--") {
			globals.CommandArgs = args[i:]
			return globals, nil
		}
		keyValue := strings.TrimPrefix(arg, "--")
		key, value, hasValue := strings.Cut(keyValue, "=")
		switch key {
		case "json":
			globals.JSONOutput = true
		case "repo", "repository-id", "daemon-socket", "capability-token", "capability-token-file", "deadline-ms":
			if !hasValue {
				if i+1 >= len(args) {
					return leadingGlobals{}, fmt.Errorf("--%s requires a value", key)
				}
				value = args[i+1]
				i++
			}
			if key == "repo" {
				globals.RepoPath = value
			}
			if key == "repository-id" {
				globals.RepositoryID = value
			}
			if key == "daemon-socket" {
				globals.SocketPath = value
			}
			if key == "capability-token" {
				globals.Token = value
			}
			if key == "capability-token-file" {
				globals.TokenFile = value
			}
			if key == "deadline-ms" {
				parsed, err := strconv.Atoi(value)
				if err != nil || parsed < 0 {
					return leadingGlobals{}, fmt.Errorf("--deadline-ms must be a non-negative integer")
				}
				globals.DeadlineMS = parsed
			}
		default:
			globals.CommandArgs = args[i:]
			return globals, nil
		}
	}
	return globals, nil
}

func containsFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func runRunDrive(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	runID := ""
	interval := 15 * time.Second
	once := false
	jsonOutput := globals.JSONOutput
	forceConcurrent := false
	providerAuthGate := string(laneproviderauth.GateAuto)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if routes.IsHelpArg(arg) {
			printRunDriveHelp(stdout)
			return 0
		}
		if !strings.HasPrefix(arg, "--") {
			if runID == "" {
				runID = arg
				continue
			}
			_, _ = fmt.Fprintf(stderr, "unexpected positional argument: %s\n", arg)
			return 2
		}
		keyValue := strings.TrimPrefix(arg, "--")
		key, value, hasValue := strings.Cut(keyValue, "=")
		switch key {
		case "run-id":
			if !hasValue {
				if i+1 >= len(args) {
					_, _ = fmt.Fprintln(stderr, "--run-id requires a value")
					return 2
				}
				value = args[i+1]
				i++
			}
			runID = value
		case "interval":
			if !hasValue {
				if i+1 >= len(args) {
					_, _ = fmt.Fprintln(stderr, "--interval requires a value")
					return 2
				}
				value = args[i+1]
				i++
			}
			parsed, err := parseDriveInterval(value)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return 2
			}
			interval = parsed
		case "once":
			once = true
		case "force-concurrent":
			forceConcurrent = true
		case "json":
			jsonOutput = true
		case "provider-auth-gate":
			if !hasValue {
				if i+1 >= len(args) {
					_, _ = fmt.Fprintln(stderr, "--provider-auth-gate requires a value")
					return 2
				}
				value = args[i+1]
				i++
			}
			mode, err := laneproviderauth.ParseGateMode(value)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, err.Error())
				return 2
			}
			providerAuthGate = string(mode)
		default:
			_, _ = fmt.Fprintf(stderr, "unknown run drive flag: --%s\n", key)
			return 2
		}
	}
	if runID == "" {
		_, _ = fmt.Fprintln(stderr, "run drive requires --run-id")
		return 2
	}
	config, err := rpcclient.ResolveConfig(os.Environ(), globals.SocketPath, globals.Token, globals.TokenFile, globals.DeadlineMS)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	client := rpcclient.Client{Config: config}
	ctx := context.Background()
	repositoryID := globals.RepositoryID
	repoRoot := globals.RepoPath
	if repositoryID == "" {
		repositoryID = envLookup(os.Environ(), "STRIATUM_REPOSITORY_ID")
	}
	if repositoryID == "" {
		if repoRoot == "" {
			if cwd, err := os.Getwd(); err == nil {
				repoRoot = cwd
			}
		}
		resolved, err := client.Invoke(ctx, "repo.resolve", map[string]any{"path": repoRoot})
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err.Error())
			return rpcclient.ExitCode(err)
		}
		repositoryID, _ = resolved["repository_id"].(string)
		if root, _ := resolved["repo_root"].(string); root != "" {
			repoRoot = root
		}
	}
	if repositoryID == "" {
		_, _ = fmt.Fprintln(stderr, "repo.resolve response did not include repository_id")
		return 1
	}
	err = rundrive.Run(ctx, client, rundrive.Options{
		RepositoryID:     repositoryID,
		RunID:            runID,
		RepoRoot:         repoRoot,
		Interval:         interval,
		Once:             once,
		JSON:             jsonOutput,
		Stdout:           stdout,
		Stderr:           stderr,
		ProviderAuthGate: providerAuthGate,
		ForceConcurrent:  forceConcurrent,
	})
	if err == nil {
		return 0
	}
	var terminal rundrive.TerminalError
	if errors.As(err, &terminal) {
		_, _ = fmt.Fprintln(stderr, terminal.Error())
		return 1
	}
	var concurrent rundrive.ConcurrentDriveError
	if errors.As(err, &concurrent) {
		_, _ = fmt.Fprintln(stderr, concurrent.Error())
		return 2
	}
	_, _ = fmt.Fprintln(stderr, err.Error())
	return rpcclient.ExitCode(err)
}

func parseDriveInterval(value string) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("--interval requires a value")
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		if parsed <= 0 {
			return 0, fmt.Errorf("--interval must be positive")
		}
		return parsed, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("--interval must be a positive duration or seconds value")
	}
	return time.Duration(seconds) * time.Second, nil
}

func printRunDriveHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum run drive --run-id <id> [--interval 15s] [--provider-auth-gate auto|required|off] [--once] [--force-concurrent] [--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Drive one run by registering and supervising lanes as queued jobs unblock.")
	_, _ = fmt.Fprintln(out, "This is a local operator loop over existing daemon RPC methods; it adds no daemon method.")
	_, _ = fmt.Fprintln(out, "Refuses to start if a live drive for the same run already holds the advisory")
	_, _ = fmt.Fprintln(out, "marker (stop that pid first); pass --force-concurrent to deliberately co-drive.")
}

func envLookup(env []string, key string) string {
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}

func runWorkflow(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	// #111: `workflow --help` (and bare `workflow`) lists the subcommands instead
	// of an "unknown flag" error.
	if len(args) == 0 || routes.IsHelpArg(args[0]) {
		out := stdout
		if len(args) == 0 {
			out = stderr
		}
		_, _ = fmt.Fprintln(out, "usage: striatum workflow {validate|generate|templates} ...")
		_, _ = fmt.Fprintln(out, "  validate <path>            validate a workflow.json against the authoring rules")
		_, _ = fmt.Fprintln(out, "  generate --shape <shape>   render a starter workflow from a catalog shape")
		_, _ = fmt.Fprintln(out, "  templates {list|show}      browse the bundled shape/lane-set catalog")
		_, _ = fmt.Fprintln(out, "Run `striatum workflow <subcommand> --help` for a subcommand's flags.")
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "validate":
		return runWorkflowValidate(args[1:], stdout, stderr, repoRootOverride)
	case "generate":
		return runWorkflowGenerate(args[1:], stdout, stderr, repoRootOverride)
	case "templates":
		return runWorkflowTemplates(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown workflow command: %s\n", args[0])
		return 2
	}
}

// runWorkflowGenerate renders a starter workflow from an embedded catalog shape.
// Preview (dry-run) by default; --write commits the repo-relative files.
func runWorkflowGenerate(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	shape := ""
	laneSet := ""
	workflowID := ""
	scaffoldRoot := ""
	artifactRoot := ""
	write := false
	jsonOutput := false
	options := map[string]any{}
	lanes := map[string]any{}
	laneModifiers := []any{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, hasValue := strings.Cut(arg, "=")
		next := func() (string, bool) {
			if hasValue {
				return value, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch key {
		case "-h", "--help", "help":
			_, _ = fmt.Fprintln(stdout, "usage: striatum workflow generate --shape <shape> [--lane-set <set>] [--workflow-id <id>] [--scaffold-root <path>] [--artifact-root <path>] [--lane-modifier <name> ...] [--option key=value ...] [--write] [--json]")
			_, _ = fmt.Fprintln(stdout, "Run `striatum workflow templates list --kind shape` for the generatable shapes.")
			return 0
		case "--shape":
			shape, _ = next()
		case "--lane-set":
			laneSet, _ = next()
		case "--workflow-id":
			workflowID, _ = next()
		case "--scaffold-root":
			scaffoldRoot, _ = next()
		case "--artifact-root":
			artifactRoot, _ = next()
		case "--lane-modifier":
			modifier, ok := next()
			if !ok || modifier == "" {
				_, _ = fmt.Fprintln(stderr, "--lane-modifier requires a value (e.g. worktree_isolated, supervised, constrained, harness_profiled)")
				return 2
			}
			laneModifiers = append(laneModifiers, modifier)
		case "--option":
			kv, ok := next()
			if !ok {
				_, _ = fmt.Fprintln(stderr, "--option requires key=value")
				return 2
			}
			optKey, optVal, ok2 := strings.Cut(kv, "=")
			if !ok2 || optKey == "" {
				_, _ = fmt.Fprintf(stderr, "--option must be key=value, got %q\n", kv)
				return 2
			}
			// #288: the catalog advertises top-level spec fields (workflow_id,
			// artifact_root) in a shape's required_options, so an operator naturally
			// reaches for `--option artifact_root=...`. Route those recognized keys to
			// the same destination as the dedicated flag instead of rejecting them as
			// unknown generator options.
			switch optKey {
			case "workflow_id":
				workflowID = optVal
				continue
			case "artifact_root":
				artifactRoot = optVal
				continue
			case "scaffold_root":
				scaffoldRoot = optVal
				continue
			}
			// #187: lane-spec keys the catalog advertises in a lane set's
			// required_options (e.g. lanes.author.command) route into spec.lanes,
			// not the options allowlist. The value for argv-array lane keys
			// (command/capabilities) is a JSON array; scalar lane keys take the
			// raw string. Any other key flows to options as before.
			if err := workflowgenerate.ApplyGenerateOption(options, lanes, optKey, optVal); err != nil {
				_, _ = fmt.Fprintf(stderr, "%s\n", err.Error())
				return 2
			}
		case "--write":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "--write must be a boolean")
				return 2
			}
			write = parsed
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				_, _ = fmt.Fprintln(stderr, "--json must be a boolean")
				return 2
			}
			jsonOutput = parsed
		default:
			_, _ = fmt.Fprintf(stderr, "unknown workflow generate flag: %s\n", arg)
			return 2
		}
	}

	if shape == "" {
		_, _ = fmt.Fprintln(stderr, "usage: striatum workflow generate --shape <shape> [--lane-set <set>] [--workflow-id <id>] [--scaffold-root <path>] [--artifact-root <path>] [--lane-modifier <name> ...] [--option key=value ...] [--write] [--json]")
		return 2
	}

	// Default to the "local" fixture lane set: it requires no real lane commands,
	// so a starter scaffold generates and validates out of the box. Other lane
	// sets (e.g. author_reviewer, single_agent) require lane commands the operator
	// supplies by editing the generated workflow or via --lane-set after wiring
	// lanes; the generator reports exactly which lane command is missing.
	if laneSet == "" {
		laneSet = "local"
	}
	if workflowID == "" {
		workflowID = strings.ReplaceAll(shape, "_", "-") + "-starter"
	}
	if scaffoldRoot == "" {
		scaffoldRoot = "docs/operator/workflows/" + workflowID
	}
	if artifactRoot == "" {
		artifactRoot = "docs/operator/artifacts/" + workflowID
	}

	spec, err := workflowgenerate.SpecFromMap(map[string]any{
		"schema_version":   workflowgenerate.GeneratorSchemaVersion,
		"shape":            shape,
		"lane_set":         laneSet,
		"workflow_id":      workflowID,
		"name":             workflowID,
		"workflow_version": time.Now().UTC().Format("2006-01-02"),
		"branch":           map[string]any{"mode": "confirm", "suggested_name": "striatum/" + workflowID, "allow_dirty": false},
		"scaffold_root":    scaffoldRoot,
		"artifact_root":    artifactRoot,
		"lanes":            lanes,
		"options":          options,
		"lane_modifiers":   laneModifiers,
		"context_docs":     []any{},
	})
	if err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_generate_invalid", err, 8)
	}
	generated, err := workflowgenerate.Generate(spec)
	if err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_generate_invalid", err, 8)
	}

	repoRoot := repoRootOverride
	if repoRoot == "" {
		repoRoot, err = os.Getwd()
		if err != nil {
			return outputWorkflowValidateError(stdout, stderr, jsonOutput, "cwd_error", err, 1)
		}
	}

	if write {
		result, err := workflowgenerate.Write(repoRoot, generated)
		if err != nil {
			return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_generate_write_failed", err, 1)
		}
		if jsonOutput {
			return writeJSON(stdout, map[string]any{"ok": true, "data": map[string]any{"shape": shape, "workflow_id": workflowID, "written": result}}, stderr)
		}
		_, _ = fmt.Fprintf(stdout, "wrote workflow %s (shape %s) under %s\n", workflowID, shape, scaffoldRoot)
		return 0
	}

	planned, err := workflowgenerate.Preview(repoRoot, generated)
	if err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_generate_preview_failed", err, 1)
	}
	if jsonOutput {
		return writeJSON(stdout, map[string]any{"ok": true, "data": map[string]any{"shape": shape, "workflow_id": workflowID, "lane_set": laneSet, "planned": planned}}, stderr)
	}
	_, _ = fmt.Fprintf(stdout, "preview: workflow %s (shape %s, lane_set %s) — %d planned file(s); pass --write to commit\n", workflowID, shape, laneSet, len(planned))
	for _, p := range planned {
		_, _ = fmt.Fprintf(stdout, "  %v %v\n", p["status"], p["path"])
	}
	return 0
}

// runWorkflowTemplates lists or shows entries from the embedded workflow catalog.
func runWorkflowTemplates(args []string, stdout io.Writer, stderr io.Writer) int {
	// The dispatcher may inject a global --json before the subcommand; treat
	// --json as order-independent and dispatch on the first non-flag arg.
	jsonOutput := false
	rest := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonOutput = true
			continue
		}
		rest = append(rest, a)
	}
	args = rest
	if len(args) == 0 || routes.IsHelpArg(args[0]) { // #111: support --help
		out := stderr
		rc := 2
		if len(args) > 0 {
			out, rc = stdout, 0
		}
		_, _ = fmt.Fprintln(out, "usage: striatum workflow templates {list|show} [--kind <kind>] [--json]")
		_, _ = fmt.Fprintln(out, "  list [--kind shape|lane_set|role_pack|adversary_pack]   browse the catalog (shapes marked example-only are not --shape generatable)")
		_, _ = fmt.Fprintln(out, "  show <template-id>                                      show one template")
		return rc
	}
	catalog, err := workflowtemplates.Load()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "load workflow template catalog: %v\n", err)
		return 1
	}
	switch args[0] {
	case "list":
		kind := ""
		for i := 1; i < len(args); i++ {
			key, value, hasValue := strings.Cut(args[i], "=")
			switch key {
			case "--kind":
				if hasValue {
					kind = value
				} else if i+1 < len(args) {
					i++
					kind = args[i]
				}
			default:
				_, _ = fmt.Fprintf(stderr, "unknown workflow templates list flag: %s\n", args[i])
				return 2
			}
		}
		entries, err := catalog.List(kind)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		if jsonOutput {
			return writeJSON(stdout, map[string]any{"ok": true, "data": map[string]any{"kind": kind, "templates": entries}}, stderr)
		}
		for _, e := range entries {
			_, _ = fmt.Fprintf(stdout, "%-22s %-14s %v\n", e["template_id"], e["kind"], e["summary"])
		}
		return 0
	case "show":
		if len(args) < 2 {
			_, _ = fmt.Fprintln(stderr, "usage: striatum workflow templates show <template-id> [--json]")
			return 2
		}
		entry, err := catalog.Get(args[1])
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		return writeJSON(stdout, map[string]any{"ok": true, "data": entry}, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown workflow templates command: %s\n", args[0])
		return 2
	}
}

func runWorkflowValidate(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	for _, a := range args { // #111: support --help instead of "unknown flag"
		if routes.IsHelpArg(a) {
			_, _ = fmt.Fprintln(stdout, "usage: striatum workflow validate [--allow-same-model-pairing] [--json] <path>")
			_, _ = fmt.Fprintln(stdout, "Validates a workflow.json against the authoring rules (phases, edges, lanes, same-model pairing).")
			return 0
		}
	}
	allowSameModel, jsonOutput, paths, err := parseWorkflowValidateArgs(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if len(paths) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: striatum workflow validate [--allow-same-model-pairing] [--json] path")
		return 2
	}
	repoRoot := repoRootOverride
	if repoRoot == "" {
		repoRoot, err = os.Getwd()
		if err != nil {
			return outputWorkflowValidateError(stdout, stderr, jsonOutput, "cwd_error", err, 1)
		}
	}
	workflow, _, err := workflowauthoring.LoadFile(repoRoot, paths[0])
	if err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_invalid", err, 8)
	}
	// Retired one-shot agent commands are hard refusals. They cannot run the
	// daemon-owned interactive work-packet loop; Claude has an explicit
	// compatibility override, Codex exec does not.
	if err := workflowauthoring.RefuseRetiredOneShotLanes(workflow); err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_lint_refused", err, 8)
	}
	if err := workflowauthoring.RefuseAutonomousSharedCheckoutRepoWrite(workflow); err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_lint_refused", err, 8)
	}
	if !allowSameModel {
		if err := refuseSameModelLint(workflow); err != nil {
			return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_lint_refused", err, 8)
		}
	}
	// RFC 0141 Pillar 3 (UNFILLED): a verification gate whose external checks are
	// sanctioned-but-unpinned on this host reads RED here, naming the entries and
	// the literal fix command — never a false green. Pure file read, no lane.
	if tb := verifier.EvaluateAllowlistTemplate(repoRoot, workflow); tb != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, tb.Reason, fmt.Errorf("%s", tb.Message), 8)
	}
	if jsonOutput {
		return writeJSON(stdout, map[string]any{
			"ok": true,
			"data": map[string]any{
				"valid":       true,
				"workflow_id": workflow["workflow_id"],
			},
		}, stderr)
	}
	_, _ = fmt.Fprintln(stdout, "valid")
	return 0
}

func parseWorkflowValidateArgs(args []string) (bool, bool, []string, error) {
	allowSameModel := false
	jsonOutput := false
	paths := []string{}
	for _, arg := range args {
		key, value, hasValue := strings.Cut(arg, "=")
		switch key {
		case "--allow-same-model-pairing":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return false, false, nil, fmt.Errorf("--allow-same-model-pairing must be a boolean")
			}
			allowSameModel = parsed
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return false, false, nil, fmt.Errorf("--json must be a boolean")
			}
			jsonOutput = parsed
		default:
			if strings.HasPrefix(arg, "-") {
				return false, false, nil, fmt.Errorf("unknown workflow validate flag: %s", arg)
			}
			paths = append(paths, arg)
		}
	}
	return allowSameModel, jsonOutput, paths, nil
}

func optionalBool(value string, hasValue bool) (bool, error) {
	if !hasValue {
		return true, nil
	}
	return strconv.ParseBool(value)
}

func refuseSameModelLint(workflow map[string]any) error {
	// Inline workflow override (parity with cycle.allow_same_model): record the
	// same-model review pairing acceptance in the workflow file itself, so a
	// panel where the builder shares a model family with a reviewer (unavoidable
	// when only N model families exist and the builder is one of them) validates
	// clean without a CLI flag.
	if workflow["allow_same_model_review_pairing"] == true {
		return nil
	}
	result, err := workflowauthoring.Lint(workflow)
	if err != nil {
		return err
	}
	for _, warning := range anySlice(result["warnings"]) {
		item, ok := warning.(map[string]any)
		if !ok {
			continue
		}
		rule, _ := item["rule"].(string)
		// RFC 0093: the adjudicator lane must differ from the holder/proposer it
		// adjudicates, with the RFC 0064 same-model refusal/override applying. The
		// same_model_adjudicator_pair lint is refused here alongside the review and
		// revision-cycle same-model pairings unless the operator records an audited
		// override.
		if rule == "same_model_review_pair" || rule == "same_model_revision_cycle" || rule == "same_model_adjudicator_pair" {
			message, _ := item["message"].(string)
			if strings.TrimSpace(message) == "" {
				message = "same-model review pairing refused"
			}
			// Make the override discoverable (this is a warning-severity lint,
			// not a hard structural error): point at the three resolutions.
			message += " — to accept this pairing: pass --allow-same-model-pairing, set \"allow_same_model_review_pairing\": true in the workflow (or cycle.allow_same_model for a revision cycle), or use an independent review lane."
			return errors.New(message)
		}
	}
	return nil
}

func outputWorkflowValidateError(stdout io.Writer, stderr io.Writer, jsonOutput bool, code string, err error, exitCode int) int {
	errObj := map[string]any{
		"code":    code,
		"message": err.Error(),
	}
	// #288: generator errors carry a copy-pasteable Hint (e.g. the JSON-array
	// `--option lanes.<id>.command=...` shape) and the offending field path. Surface
	// them so the operator gets the fix in one shot instead of guess-and-retry.
	var genErr *workflowgenerate.Error
	if errors.As(err, &genErr) {
		if genErr.FieldPath != "" {
			errObj["field"] = genErr.FieldPath
		}
		if genErr.Hint != "" {
			errObj["hint"] = genErr.Hint
		}
		if genErr.Ref != "" {
			errObj["ref"] = genErr.Ref
		}
	}
	if jsonOutput {
		_ = writeJSON(stdout, map[string]any{"ok": false, "error": errObj}, stderr)
		return exitCode
	}
	_, _ = fmt.Fprintln(stderr, err.Error())
	if genErr != nil && genErr.Hint != "" {
		_, _ = fmt.Fprintln(stderr, "hint: "+genErr.Hint)
	}
	return exitCode
}

func writeJSON(stdout io.Writer, payload map[string]any, stderr io.Writer) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	_, _ = fmt.Fprintln(stdout, string(encoded))
	return 0
}

func anySlice(value any) []any {
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
