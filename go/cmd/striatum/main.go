package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/cli/dispatch"
	"github.com/halbritt/striatum/go/pkg/cli/localcommands"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	cliskills "github.com/halbritt/striatum/go/pkg/cli/skills"
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
		fmt.Fprintln(stderr, err.Error())
		return 2
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
		fmt.Fprintln(stdout, version)
		return 0
	default:
		return runDaemonRoute(args, stdout, stderr)
	}
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "usage: striatum [--version] [--repo path|--repository-id id] command ...")
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
	CommandArgs []string
	RepoPath    string
	JSONOutput  bool
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

func runWorkflow(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: striatum workflow {validate|generate|templates} ...")
		return 2
	}
	switch args[0] {
	case "validate":
		return runWorkflowValidate(args[1:], stdout, stderr, repoRootOverride)
	case "generate":
		return runWorkflowGenerate(args[1:], stdout, stderr, repoRootOverride)
	case "templates":
		return runWorkflowTemplates(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown workflow command: %s\n", args[0])
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
		case "--option":
			kv, ok := next()
			if !ok {
				fmt.Fprintln(stderr, "--option requires key=value")
				return 2
			}
			optKey, optVal, ok2 := strings.Cut(kv, "=")
			if !ok2 || optKey == "" {
				fmt.Fprintf(stderr, "--option must be key=value, got %q\n", kv)
				return 2
			}
			options[optKey] = optVal
		case "--write":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				fmt.Fprintln(stderr, "--write must be a boolean")
				return 2
			}
			write = parsed
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				fmt.Fprintln(stderr, "--json must be a boolean")
				return 2
			}
			jsonOutput = parsed
		default:
			fmt.Fprintf(stderr, "unknown workflow generate flag: %s\n", arg)
			return 2
		}
	}

	if shape == "" {
		fmt.Fprintln(stderr, "usage: striatum workflow generate --shape <shape> [--lane-set <set>] [--workflow-id <id>] [--scaffold-root <path>] [--artifact-root <path>] [--option key=value ...] [--write] [--json]")
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
		"lanes":            map[string]any{},
		"options":          options,
		"lane_modifiers":   []any{},
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
		fmt.Fprintf(stdout, "wrote workflow %s (shape %s) under %s\n", workflowID, shape, scaffoldRoot)
		return 0
	}

	planned, err := workflowgenerate.Preview(repoRoot, generated)
	if err != nil {
		return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_generate_preview_failed", err, 1)
	}
	if jsonOutput {
		return writeJSON(stdout, map[string]any{"ok": true, "data": map[string]any{"shape": shape, "workflow_id": workflowID, "lane_set": laneSet, "planned": planned}}, stderr)
	}
	fmt.Fprintf(stdout, "preview: workflow %s (shape %s, lane_set %s) — %d planned file(s); pass --write to commit\n", workflowID, shape, laneSet, len(planned))
	for _, p := range planned {
		fmt.Fprintf(stdout, "  %v %v\n", p["status"], p["path"])
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
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: striatum workflow templates {list|show} [--kind <kind>] [--json]")
		return 2
	}
	catalog, err := workflowtemplates.Load()
	if err != nil {
		fmt.Fprintf(stderr, "load workflow template catalog: %v\n", err)
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
				fmt.Fprintf(stderr, "unknown workflow templates list flag: %s\n", args[i])
				return 2
			}
		}
		entries, err := catalog.List(kind)
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		if jsonOutput {
			return writeJSON(stdout, map[string]any{"ok": true, "data": map[string]any{"kind": kind, "templates": entries}}, stderr)
		}
		for _, e := range entries {
			fmt.Fprintf(stdout, "%-22s %-14s %v\n", e["template_id"], e["kind"], e["summary"])
		}
		return 0
	case "show":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "usage: striatum workflow templates show <template-id> [--json]")
			return 2
		}
		entry, err := catalog.Get(args[1])
		if err != nil {
			fmt.Fprintf(stderr, "%v\n", err)
			return 2
		}
		return writeJSON(stdout, map[string]any{"ok": true, "data": entry}, stderr)
	default:
		fmt.Fprintf(stderr, "unknown workflow templates command: %s\n", args[0])
		return 2
	}
}

func runWorkflowValidate(args []string, stdout io.Writer, stderr io.Writer, repoRootOverride string) int {
	allowSameModel, jsonOutput, paths, err := parseWorkflowValidateArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if len(paths) != 1 {
		fmt.Fprintln(stderr, "usage: striatum workflow validate [--allow-same-model-pairing] [--json] path")
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
	if !allowSameModel {
		if err := refuseSameModelLint(workflow); err != nil {
			return outputWorkflowValidateError(stdout, stderr, jsonOutput, "workflow_lint_refused", err, 8)
		}
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
	fmt.Fprintln(stdout, "valid")
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
		if rule == "same_model_review_pair" || rule == "same_model_revision_cycle" {
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
	if jsonOutput {
		_ = writeJSON(stdout, map[string]any{
			"ok": false,
			"error": map[string]any{
				"code":    code,
				"message": err.Error(),
			},
		}, stderr)
		return exitCode
	}
	fmt.Fprintln(stderr, err.Error())
	return exitCode
}

func writeJSON(stdout io.Writer, payload map[string]any, stderr io.Writer) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	fmt.Fprintln(stdout, string(encoded))
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
