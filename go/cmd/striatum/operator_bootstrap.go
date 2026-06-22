package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/admin"
	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
)

const operatorBootstrapSchemaVersion = "striatum.operator_bootstrap.v1"

type operatorBootstrapOptions struct {
	JSON             bool
	Markdown         bool
	OperatorDocsRoot string
	Limit            int
}

type operatorBootstrapPacket struct {
	SchemaVersion string                    `json:"schema_version"`
	OK            bool                      `json:"ok"`
	GeneratedAt   string                    `json:"generated_at"`
	RoleContract  operatorRoleContract      `json:"role_contract"`
	Boundaries    []string                  `json:"boundaries"`
	Repository    operatorRepositorySummary `json:"repository"`
	Daemon        operatorDaemonSummary     `json:"daemon"`
	Doctor        operatorDoctorSummary     `json:"doctor"`
	Frontier      operatorFrontierSummary   `json:"frontier"`
	OperatorBrief operatorBriefSummary      `json:"operator_brief"`
	Skills        operatorSkillsSummary     `json:"skills"`
	NextActions   []string                  `json:"next_actions"`
	ReadingPlan   []string                  `json:"reading_plan"`
	Limits        map[string]int            `json:"limits"`
}

type operatorRoleContract struct {
	Role       string   `json:"role"`
	Summary    string   `json:"summary"`
	References []string `json:"references"`
}

type operatorRepositorySummary struct {
	Root         string   `json:"root"`
	RepositoryID string   `json:"repository_id,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	Head         string   `json:"head,omitempty"`
	DirtyState   string   `json:"dirty_state"`
	DirtyCount   int      `json:"dirty_count"`
	DirtySample  []string `json:"dirty_sample,omitempty"`
	Version      string   `json:"version,omitempty"`
	LatestTag    string   `json:"latest_tag,omitempty"`
	ProbeErrors  []string `json:"probe_errors,omitempty"`
}

type operatorDaemonSummary struct {
	Reachable        bool     `json:"reachable"`
	Authorized       bool     `json:"authorized"`
	SocketPath       string   `json:"socket_path,omitempty"`
	TokenSource      string   `json:"token_source,omitempty"`
	TokenPresent     bool     `json:"token_present"`
	MCPEndpointPath  string   `json:"mcp_endpoint_path,omitempty"`
	MCPEndpointFound bool     `json:"mcp_endpoint_found"`
	SchemaVersion    any      `json:"schema_version,omitempty"`
	Error            string   `json:"error,omitempty"`
	RecoveryCommands []string `json:"recovery_commands,omitempty"`
}

type operatorDoctorSummary struct {
	Available         bool     `json:"available"`
	OK                bool     `json:"ok"`
	ProblemCount      int      `json:"problem_count"`
	WarningCount      int      `json:"warning_count"`
	Problems          []string `json:"problems,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	StaleLeases       int      `json:"stale_leases"`
	WaitingHuman      int      `json:"waiting_human"`
	NeedsOperator     int      `json:"needs_operator"`
	NeedsOperatorRuns []string `json:"needs_operator_runs,omitempty"`
}

type operatorFrontierSummary struct {
	Available            bool                  `json:"available"`
	RunCounts            map[string]int        `json:"run_counts"`
	ActiveRuns           []operatorRunItem     `json:"active_runs,omitempty"`
	ClaimableCount       int                   `json:"claimable_count"`
	ClaimableJobs        []operatorJobItem     `json:"claimable_jobs,omitempty"`
	OpenBlockerCount     int                   `json:"open_blocker_count"`
	OpenBlockers         []operatorBlockerItem `json:"open_blockers,omitempty"`
	HumanCheckpointCount int                   `json:"human_checkpoint_count"`
	HumanCheckpoints     []operatorBlockerItem `json:"human_checkpoints,omitempty"`
	DaemonNextActions    []string              `json:"daemon_next_actions,omitempty"`
}

type operatorRunItem struct {
	RunID      string `json:"run_id"`
	State      string `json:"state"`
	BranchName string `json:"branch_name,omitempty"`
}

type operatorJobItem struct {
	RunID  string `json:"run_id,omitempty"`
	JobID  string `json:"job_id,omitempty"`
	State  string `json:"state,omitempty"`
	RoleID string `json:"role_id,omitempty"`
	LaneID string `json:"lane_id,omitempty"`
}

type operatorBlockerItem struct {
	BlockerID   string `json:"blocker_id,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	JobID       string `json:"job_id,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Description string `json:"description,omitempty"`
}

type operatorBriefSummary struct {
	Status             string   `json:"status"`
	Path               string   `json:"path"`
	BriefID            string   `json:"brief_id,omitempty"`
	DeclaredStatus     string   `json:"declared_status,omitempty"`
	ContextBudgetLines int      `json:"context_budget_lines,omitempty"`
	RetrievalPriority  string   `json:"retrieval_priority,omitempty"`
	ScopeLinks         []string `json:"scope_links,omitempty"`
	Evidence           []string `json:"evidence,omitempty"`
	Error              string   `json:"error,omitempty"`
}

type operatorSkillsSummary struct {
	Checked          bool     `json:"checked"`
	Status           string   `json:"status"`
	CurrentVersion   string   `json:"current_version,omitempty"`
	WarningCount     int      `json:"warning_count"`
	Warnings         []string `json:"warnings,omitempty"`
	RecoveryCommands []string `json:"recovery_commands,omitempty"`
}

func runOperator(args []string, stdout io.Writer, stderr io.Writer, globals leadingGlobals) int {
	if len(args) == 0 || routesHelp(args[0]) {
		printOperatorHelp(stdout)
		if len(args) == 0 {
			return 2
		}
		return 0
	}
	switch args[0] {
	case "bootstrap":
		options, err := parseOperatorBootstrapArgs(args[1:])
		if err != nil {
			var helpErr errOperatorBootstrapHelp
			if errors.As(err, &helpErr) {
				printOperatorHelp(stdout)
				return 0
			}
			_, _ = fmt.Fprintln(stderr, err.Error())
			return 2
		}
		packet := buildOperatorBootstrap(context.Background(), globals, options)
		if options.JSON {
			return writeJSONValue(stdout, packet, stderr)
		}
		_, _ = fmt.Fprint(stdout, renderOperatorBootstrap(packet))
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown operator command: %s\n", args[0])
		return 2
	}
}

func routesHelp(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func printOperatorHelp(out io.Writer) {
	_, _ = fmt.Fprintln(out, "usage: striatum operator bootstrap [--operator-docs-root <path>] [--limit N] [--markdown|--json]")
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, "Read-only bounded cold-start packet for an AI operator.")
	_, _ = fmt.Fprintln(out, "Composes daemon reads (repo.resolve, status, doctor) with local repo/doc probes.")
}

func parseOperatorBootstrapArgs(args []string) (operatorBootstrapOptions, error) {
	options := operatorBootstrapOptions{Markdown: true, Limit: 5}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if routesHelp(arg) {
			return operatorBootstrapOptions{}, errOperatorBootstrapHelp{}
		}
		key, value, hasValue := strings.Cut(arg, "=")
		next := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", key)
			}
			i++
			return args[i], nil
		}
		switch key {
		case "--json":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return options, fmt.Errorf("--json must be a boolean")
			}
			options.JSON = parsed
			if parsed {
				options.Markdown = false
			}
		case "--markdown":
			parsed, err := optionalBool(value, hasValue)
			if err != nil {
				return options, fmt.Errorf("--markdown must be a boolean")
			}
			if !parsed {
				return options, fmt.Errorf("--markdown=false is not supported; use --json for JSON output")
			}
			options.Markdown = parsed
			options.JSON = false
		case "--operator-docs-root":
			parsed, err := next()
			if err != nil {
				return options, err
			}
			options.OperatorDocsRoot = parsed
		case "--limit":
			parsed, err := next()
			if err != nil {
				return options, err
			}
			n, err := strconv.Atoi(parsed)
			if err != nil || n < 1 {
				return options, fmt.Errorf("--limit must be a positive integer")
			}
			if n > 20 {
				n = 20
			}
			options.Limit = n
		default:
			return options, fmt.Errorf("unknown operator bootstrap flag: %s", arg)
		}
	}
	return options, nil
}

type errOperatorBootstrapHelp struct{}

func (errOperatorBootstrapHelp) Error() string { return "operator bootstrap help requested" }

func buildOperatorBootstrap(ctx context.Context, globals leadingGlobals, options operatorBootstrapOptions) operatorBootstrapPacket {
	if options.Limit <= 0 {
		options.Limit = 5
	}
	repoRoot := resolveBootstrapRepoRoot(globals.RepoPath)
	packet := operatorBootstrapPacket{
		SchemaVersion: operatorBootstrapSchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		RoleContract: operatorRoleContract{
			Role:    "ai_operator",
			Summary: "Drive Striatum through daemon MCP/RPC, keep human-principal escalation separate, and do not treat repository files as live state.",
			References: []string{
				"AGENTS.md",
				"docs/how-to/how-to-agent.md",
				"docs/reference/spec.md",
			},
		},
		Boundaries: []string{
			"daemon-owned PostgreSQL is authoritative live state",
			"repository files are durable provenance, not the live message bus",
			".striatum/, tmux output, marker files, and transcripts are not workflow state",
			"CLI verbs are daemon-backed bootstrap, diagnostics, or compatibility clients",
		},
		Repository: summarizeBootstrapRepository(ctx, repoRoot, options.Limit*2),
		Daemon:     summarizeBootstrapDaemon(globals),
		Limits: map[string]int{
			"list_items":           options.Limit,
			"dirty_sample":         options.Limit * 2,
			"status_run_limit":     0,
			"human_output_lines":   200,
			"json_embeds_full_rpc": 0,
		},
	}
	packet.OperatorBrief = summarizeOperatorBrief(repoRoot, options.OperatorDocsRoot, packet.Repository.Version, packet.Repository.LatestTag, options.Limit+3)
	client, configOK := bootstrapRPCClient(globals, &packet)
	if configOK {
		repositoryID := strings.TrimSpace(globals.RepositoryID)
		if repositoryID == "" {
			repositoryID = envLookup(os.Environ(), "STRIATUM_REPOSITORY_ID")
		}
		if repositoryID == "" {
			repoResolve, err := client.Invoke(ctx, "repo.resolve", map[string]any{"path": repoRoot})
			if err != nil {
				packet.Daemon.Reachable = !isDaemonUnreachable(err)
				packet.Daemon.Authorized = !isDaemonAuthError(err)
				packet.Daemon.Error = err.Error()
			} else {
				packet.Daemon.Reachable = true
				packet.Daemon.Authorized = true
				repositoryID = stringField(repoResolve, "repository_id")
				if resolvedRoot := stringField(repoResolve, "repo_root"); resolvedRoot != "" {
					packet.Repository.Root = resolvedRoot
				}
			}
		}
		if repositoryID != "" {
			packet.Repository.RepositoryID = repositoryID
			status, err := client.Invoke(ctx, "status", map[string]any{"repository_id": repositoryID, "run_limit": 0})
			if err != nil {
				packet.Daemon.Reachable = !isDaemonUnreachable(err)
				packet.Daemon.Authorized = !isDaemonAuthError(err)
				if packet.Daemon.Error == "" {
					packet.Daemon.Error = err.Error()
				}
			} else {
				packet.Daemon.Reachable = true
				packet.Daemon.Authorized = true
				packet.Frontier = summarizeStatus(status, options.Limit)
			}
			doctor, err := client.Invoke(ctx, "doctor", map[string]any{"repository_id": repositoryID})
			if err != nil {
				packet.Daemon.Reachable = !isDaemonUnreachable(err)
				packet.Daemon.Authorized = !isDaemonAuthError(err)
				if packet.Daemon.Error == "" {
					packet.Daemon.Error = err.Error()
				}
			} else {
				packet.Daemon.Reachable = true
				packet.Daemon.Authorized = true
				packet.Daemon.SchemaVersion = doctor["schema_version"]
				packet.Doctor = summarizeDoctor(doctor, options.Limit)
				packet.Skills = summarizeSkills(doctor, options.Limit)
			}
		}
	}
	if !packet.Frontier.Available {
		packet.Frontier.RunCounts = map[string]int{}
	}
	if !packet.Doctor.Available {
		packet.Doctor = operatorDoctorSummary{}
	}
	if packet.Skills.Status == "" {
		packet.Skills.Status = "unknown"
	}
	packet.NextActions = bootstrapNextActions(packet)
	packet.ReadingPlan = bootstrapReadingPlan(packet)
	packet.OK = packet.Daemon.Reachable && packet.Daemon.Authorized && (packet.Doctor.Available && packet.Doctor.OK)
	return packet
}

func bootstrapRPCClient(globals leadingGlobals, packet *operatorBootstrapPacket) (rpcclient.Client, bool) {
	config, err := rpcclient.ResolveConfig(os.Environ(), globals.SocketPath, globals.Token, globals.TokenFile, globals.DeadlineMS)
	if err != nil {
		packet.Daemon.Error = err.Error()
		packet.Daemon.RecoveryCommands = []string{"striatum daemon status", "striatum daemon install"}
		return rpcclient.Client{}, false
	}
	packet.Daemon.SocketPath = config.SocketPath
	packet.Daemon.TokenSource, packet.Daemon.TokenPresent = bootstrapTokenSource(config)
	if endpointPath, err := admin.RuntimeMCPEndpointPath(); err == nil {
		packet.Daemon.MCPEndpointPath = endpointPath
		if _, statErr := os.Stat(endpointPath); statErr == nil {
			packet.Daemon.MCPEndpointFound = true
		}
	}
	packet.Daemon.RecoveryCommands = []string{"striatum daemon status", "striatum doctor --json"}
	return rpcclient.Client{Config: config}, true
}

func bootstrapTokenSource(config rpcclient.Config) (string, bool) {
	if strings.TrimSpace(config.Token) != "" {
		return "inline", true
	}
	if strings.TrimSpace(config.TokenFile) == "" {
		return "", false
	}
	if _, err := os.Stat(config.TokenFile); err == nil {
		return config.TokenFile, true
	}
	return config.TokenFile, false
}

func summarizeBootstrapDaemon(globals leadingGlobals) operatorDaemonSummary {
	summary := operatorDaemonSummary{}
	if globals.SocketPath != "" {
		summary.SocketPath = globals.SocketPath
	}
	if globals.Token != "" {
		summary.TokenSource = "inline"
		summary.TokenPresent = true
	} else if globals.TokenFile != "" {
		summary.TokenSource = globals.TokenFile
		if _, err := os.Stat(globals.TokenFile); err == nil {
			summary.TokenPresent = true
		}
	}
	if endpointPath, err := admin.RuntimeMCPEndpointPath(); err == nil {
		summary.MCPEndpointPath = endpointPath
		if _, err := os.Stat(endpointPath); err == nil {
			summary.MCPEndpointFound = true
		}
	}
	return summary
}

func summarizeBootstrapRepository(ctx context.Context, repoRoot string, dirtyLimit int) operatorRepositorySummary {
	summary := operatorRepositorySummary{Root: repoRoot, DirtyState: "unknown"}
	if version, err := os.ReadFile(filepath.Join(repoRoot, "VERSION")); err == nil {
		summary.Version = strings.TrimSpace(string(version))
	}
	if branch, err := gitOutput(ctx, repoRoot, "branch", "--show-current"); err == nil {
		summary.Branch = branch
	} else {
		summary.ProbeErrors = append(summary.ProbeErrors, "git branch: "+err.Error())
	}
	if head, err := gitOutput(ctx, repoRoot, "rev-parse", "--short", "HEAD"); err == nil {
		summary.Head = head
	} else {
		summary.ProbeErrors = append(summary.ProbeErrors, "git head: "+err.Error())
	}
	if tag, err := gitOutput(ctx, repoRoot, "describe", "--tags", "--abbrev=0"); err == nil {
		summary.LatestTag = tag
	}
	if status, err := gitOutput(ctx, repoRoot, "status", "--porcelain", "--untracked-files=all"); err == nil {
		lines := nonEmptyLines(status)
		summary.DirtyCount = len(lines)
		if len(lines) == 0 {
			summary.DirtyState = "clean"
		} else {
			summary.DirtyState = "dirty"
			summary.DirtySample = capStrings(lines, dirtyLimit)
		}
	} else {
		summary.ProbeErrors = append(summary.ProbeErrors, "git status: "+err.Error())
	}
	return summary
}

func resolveBootstrapRepoRoot(repoPath string) string {
	if repoPath == "" {
		cwd, err := os.Getwd()
		if err == nil {
			repoPath = cwd
		}
	}
	if repoPath == "" {
		return "."
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return repoPath
	}
	if root, err := gitOutput(context.Background(), abs, "rev-parse", "--show-toplevel"); err == nil && root != "" {
		return root
	}
	return abs
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	allArgs := append([]string{"-C", repoRoot}, args...)
	cmd := exec.CommandContext(probeCtx, "git", allArgs...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func summarizeOperatorBrief(repoRoot string, docsRoot string, version string, latestTag string, limit int) operatorBriefSummary {
	if docsRoot == "" {
		docsRoot = filepath.Join(repoRoot, "docs", "operator")
	} else if !filepath.IsAbs(docsRoot) {
		docsRoot = filepath.Join(repoRoot, docsRoot)
	}
	path := filepath.Join(docsRoot, "BRIEF.md")
	summary := operatorBriefSummary{Status: "unknown", Path: path}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		summary.Status = "missing"
		summary.Evidence = []string{"BRIEF.md not found"}
		return summary
	}
	if err != nil {
		summary.Error = err.Error()
		summary.Evidence = []string{"BRIEF.md could not be inspected"}
		return summary
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		summary.Status = "unknown"
		summary.Error = "BRIEF.md is not a regular file"
		summary.Evidence = []string{"BRIEF.md is not a regular file"}
		return summary
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		summary.Error = err.Error()
		summary.Evidence = []string{"BRIEF.md could not be read"}
		return summary
	}
	parsed, err := artifactcontracts.ParseAndValidateFrontMatter("operator_brief", path, payload)
	if err != nil {
		summary.Status = "unknown"
		summary.Error = err.Error()
		summary.Evidence = []string{"operator_brief front matter validation failed"}
		return summary
	}
	summary.BriefID = stringAny(parsed["brief_id"])
	summary.DeclaredStatus = stringAny(parsed["status"])
	summary.RetrievalPriority = stringAny(parsed["retrieval_priority"])
	summary.ContextBudgetLines = intAny(parsed["context_budget_lines"])
	summary.ScopeLinks = capStrings(stringListAny(parsed["scope_links"]), limit)
	if summary.DeclaredStatus != "current" {
		summary.Status = "stale"
		summary.Evidence = append(summary.Evidence, "front matter status is "+summary.DeclaredStatus)
		return summary
	}
	text := string(payload)
	versionNeedle := ""
	if version != "" {
		versionNeedle = "v" + strings.TrimPrefix(version, "v")
		if !strings.Contains(text, versionNeedle) {
			summary.Status = "stale"
			summary.Evidence = append(summary.Evidence, "brief does not mention VERSION "+versionNeedle)
		}
	}
	if latestTag != "" && !strings.Contains(text, latestTag) {
		if summary.Status == "" || summary.Status == "unknown" {
			summary.Status = "stale"
		}
		summary.Evidence = append(summary.Evidence, "brief does not mention latest tag "+latestTag)
	}
	if len(summary.Evidence) == 0 {
		summary.Status = "current"
		if versionNeedle != "" {
			summary.Evidence = append(summary.Evidence, "front matter is current and mentions "+versionNeedle)
		} else {
			summary.Evidence = append(summary.Evidence, "front matter is current")
		}
	}
	return summary
}

func summarizeStatus(status map[string]any, limit int) operatorFrontierSummary {
	frontier := operatorFrontierSummary{Available: true, RunCounts: map[string]int{}}
	for _, item := range mapSlice(status["runs"]) {
		state := stringField(item, "state")
		if state == "" {
			state = "unknown"
		}
		frontier.RunCounts[state]++
		if len(frontier.ActiveRuns) < limit && !terminalRunState(state) {
			frontier.ActiveRuns = append(frontier.ActiveRuns, operatorRunItem{
				RunID:      stringField(item, "run_id"),
				State:      state,
				BranchName: stringField(item, "branch_name"),
			})
		}
	}
	claimable := mapSlice(status["claimable_jobs"])
	frontier.ClaimableCount = len(claimable)
	for _, item := range claimable {
		if len(frontier.ClaimableJobs) >= limit {
			break
		}
		frontier.ClaimableJobs = append(frontier.ClaimableJobs, operatorJobItem{
			RunID:  stringField(item, "run_id"),
			JobID:  stringField(item, "job_id"),
			State:  stringField(item, "state"),
			RoleID: stringField(item, "role_id"),
			LaneID: stringField(item, "lane_id"),
		})
	}
	openBlockers := mapSlice(status["open_blockers"])
	frontier.OpenBlockerCount = len(openBlockers)
	for _, item := range openBlockers {
		if len(frontier.OpenBlockers) >= limit {
			break
		}
		frontier.OpenBlockers = append(frontier.OpenBlockers, blockerItem(item))
	}
	checkpoints := mapSlice(status["human_checkpoints"])
	frontier.HumanCheckpointCount = len(checkpoints)
	for _, item := range checkpoints {
		if len(frontier.HumanCheckpoints) >= limit {
			break
		}
		frontier.HumanCheckpoints = append(frontier.HumanCheckpoints, blockerItem(item))
	}
	frontier.DaemonNextActions = capStrings(stringListAny(status["next_actions"]), limit)
	return frontier
}

func summarizeDoctor(doctor map[string]any, limit int) operatorDoctorSummary {
	problems := stringListAny(doctor["problems"])
	warnings := stringListAny(doctor["warnings"])
	return operatorDoctorSummary{
		Available:         true,
		OK:                boolAny(doctor["ok"]),
		ProblemCount:      len(problems),
		WarningCount:      len(warnings),
		Problems:          capStrings(problems, limit),
		Warnings:          capStrings(warnings, limit),
		StaleLeases:       intAny(doctor["stale_leases"]),
		WaitingHuman:      intAny(doctor["waiting_human"]),
		NeedsOperator:     intAny(doctor["needs_operator"]),
		NeedsOperatorRuns: capStrings(stringListAny(doctor["needs_operator_runs"]), limit),
	}
}

func summarizeSkills(doctor map[string]any, limit int) operatorSkillsSummary {
	summary := operatorSkillsSummary{Status: "unknown"}
	skills, _ := doctor["skills"].(map[string]any)
	if len(skills) > 0 {
		summary.Checked = boolAny(skills["checked"])
		summary.CurrentVersion = stringAny(skills["current_version"])
	}
	warnings := []string{}
	for _, warning := range stringListAny(doctor["warnings"]) {
		if strings.HasPrefix(warning, "skills_") || strings.Contains(warning, "skill") {
			warnings = append(warnings, warning)
		}
	}
	summary.WarningCount = len(warnings)
	summary.Warnings = capStrings(warnings, limit)
	switch {
	case len(warnings) > 0:
		summary.Status = "attention_needed"
		summary.RecoveryCommands = []string{"striatum skills install --profile all --force"}
	case summary.Checked:
		summary.Status = "ok"
	default:
		summary.Status = "unknown"
	}
	return summary
}

func bootstrapNextActions(packet operatorBootstrapPacket) []string {
	actions := []string{}
	if !packet.Daemon.Reachable {
		return []string{"striatum daemon status", "striatum daemon install", "striatum doctor --json"}
	}
	if !packet.Daemon.Authorized {
		return []string{"striatum daemon status", "striatum daemon token-create --help"}
	}
	if packet.Repository.RepositoryID == "" {
		return []string{"striatum repo add --init " + shellQuote(packet.Repository.Root), "striatum doctor --json"}
	}
	for _, runID := range packet.Doctor.NeedsOperatorRuns {
		actions = append(actions, "striatum dashboard --run-id "+runID+" --once")
		actions = append(actions, "striatum escalation list --json")
	}
	for _, run := range packet.Frontier.ActiveRuns {
		if run.RunID == "" {
			continue
		}
		actions = append(actions, "striatum dashboard --run-id "+run.RunID+" --once")
		if run.State == "running" {
			actions = append(actions, "striatum run drive --run-id "+run.RunID+" --once")
		}
	}
	if packet.Frontier.HumanCheckpointCount > 0 {
		actions = append(actions, "striatum checkpoint resolve --help")
	}
	// RFC 0142 Layer 2: a pending owner bundle would halt the daemon at the next
	// restart with awaiting_owner_ddl. Hoist the exact out-of-band remediation as
	// a next_action so the operator applies it BEFORE restarting, rather than
	// discovering it as a boot-time halt.
	for _, problem := range packet.Doctor.Problems {
		if strings.HasPrefix(problem, "owner_bundle_watermark_shortfall") {
			actions = append(actions, "striatum daemon owner-ddl apply")
			break
		}
	}
	if packet.Doctor.ProblemCount > 0 {
		actions = append(actions, "striatum doctor --json")
	}
	if packet.Skills.Status == "attention_needed" {
		actions = append(actions, packet.Skills.RecoveryCommands...)
	}
	if len(actions) == 0 {
		actions = append(actions, "striatum status --json --run-limit 0")
	}
	return dedupeStrings(actions, 8)
}

func bootstrapReadingPlan(packet operatorBootstrapPacket) []string {
	plan := []string{"docs/operator/BRIEF.md", "docs/how-to/how-to-agent.md"}
	if !packet.Daemon.Reachable || packet.Repository.RepositoryID == "" {
		plan = append(plan, "docs/how-to/postgres-transition.md")
	}
	if packet.Doctor.ProblemCount > 0 {
		plan = append(plan, "docs/reference/command-authority-matrix.md")
	}
	if packet.OperatorBrief.Status == "stale" {
		plan = append(plan, "CHANGELOG.md")
	}
	return dedupeStrings(plan, 6)
}

func renderOperatorBootstrap(packet operatorBootstrapPacket) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Striatum Operator Bootstrap\n\n")
	fmt.Fprintf(&b, "schema: %s\n", packet.SchemaVersion)
	fmt.Fprintf(&b, "generated_at: %s\n\n", packet.GeneratedAt)
	fmt.Fprintf(&b, "## Role\n\n")
	fmt.Fprintf(&b, "- %s: %s\n", packet.RoleContract.Role, packet.RoleContract.Summary)
	for _, boundary := range packet.Boundaries {
		fmt.Fprintf(&b, "- %s\n", boundary)
	}
	fmt.Fprintf(&b, "\n## Repository\n\n")
	fmt.Fprintf(&b, "- root: %s\n", packet.Repository.Root)
	if packet.Repository.RepositoryID != "" {
		fmt.Fprintf(&b, "- repository_id: %s\n", packet.Repository.RepositoryID)
	}
	fmt.Fprintf(&b, "- git: branch=%s head=%s dirty=%s (%d)\n", blank(packet.Repository.Branch), blank(packet.Repository.Head), packet.Repository.DirtyState, packet.Repository.DirtyCount)
	if packet.Repository.Version != "" || packet.Repository.LatestTag != "" {
		fmt.Fprintf(&b, "- version: VERSION=%s latest_tag=%s\n", blank(packet.Repository.Version), blank(packet.Repository.LatestTag))
	}
	for _, item := range packet.Repository.DirtySample {
		fmt.Fprintf(&b, "  - %s\n", item)
	}
	fmt.Fprintf(&b, "\n## Daemon\n\n")
	fmt.Fprintf(&b, "- reachable: %t\n", packet.Daemon.Reachable)
	fmt.Fprintf(&b, "- authorized: %t\n", packet.Daemon.Authorized)
	fmt.Fprintf(&b, "- socket: %s\n", blank(packet.Daemon.SocketPath))
	fmt.Fprintf(&b, "- token_source: %s present=%t\n", blank(packet.Daemon.TokenSource), packet.Daemon.TokenPresent)
	fmt.Fprintf(&b, "- mcp_endpoint: %s present=%t\n", blank(packet.Daemon.MCPEndpointPath), packet.Daemon.MCPEndpointFound)
	if packet.Daemon.Error != "" {
		fmt.Fprintf(&b, "- error: %s\n", packet.Daemon.Error)
	}
	fmt.Fprintf(&b, "\n## Frontier\n\n")
	fmt.Fprintf(&b, "- active_runs: %d\n", len(packet.Frontier.ActiveRuns))
	fmt.Fprintf(&b, "- claimable_jobs: %d\n", packet.Frontier.ClaimableCount)
	fmt.Fprintf(&b, "- open_blockers: %d\n", packet.Frontier.OpenBlockerCount)
	fmt.Fprintf(&b, "- human_checkpoints: %d\n", packet.Frontier.HumanCheckpointCount)
	for _, run := range packet.Frontier.ActiveRuns {
		fmt.Fprintf(&b, "  - %s %s\n", run.State, run.RunID)
	}
	fmt.Fprintf(&b, "\n## Doctor\n\n")
	if packet.Doctor.Available {
		fmt.Fprintf(&b, "- ok: %t problems=%d warnings=%d stale_leases=%d waiting_human=%d needs_operator=%d\n", packet.Doctor.OK, packet.Doctor.ProblemCount, packet.Doctor.WarningCount, packet.Doctor.StaleLeases, packet.Doctor.WaitingHuman, packet.Doctor.NeedsOperator)
		for _, problem := range packet.Doctor.Problems {
			fmt.Fprintf(&b, "  - problem: %s\n", problem)
		}
		for _, warning := range packet.Doctor.Warnings {
			fmt.Fprintf(&b, "  - warning: %s\n", warning)
		}
	} else {
		fmt.Fprintf(&b, "- unavailable\n")
	}
	fmt.Fprintf(&b, "\n## Operator Brief\n\n")
	fmt.Fprintf(&b, "- status: %s\n", packet.OperatorBrief.Status)
	fmt.Fprintf(&b, "- path: %s\n", packet.OperatorBrief.Path)
	if packet.OperatorBrief.BriefID != "" {
		fmt.Fprintf(&b, "- brief_id: %s\n", packet.OperatorBrief.BriefID)
	}
	for _, evidence := range packet.OperatorBrief.Evidence {
		fmt.Fprintf(&b, "  - %s\n", evidence)
	}
	if packet.OperatorBrief.Error != "" {
		fmt.Fprintf(&b, "  - error: %s\n", packet.OperatorBrief.Error)
	}
	fmt.Fprintf(&b, "\n## Skills\n\n")
	fmt.Fprintf(&b, "- status: %s checked=%t warnings=%d\n", packet.Skills.Status, packet.Skills.Checked, packet.Skills.WarningCount)
	for _, warning := range packet.Skills.Warnings {
		fmt.Fprintf(&b, "  - %s\n", warning)
	}
	fmt.Fprintf(&b, "\n## Next Actions\n\n")
	for _, action := range packet.NextActions {
		fmt.Fprintf(&b, "- %s\n", action)
	}
	fmt.Fprintf(&b, "\n## Reading Plan\n\n")
	for _, path := range packet.ReadingPlan {
		fmt.Fprintf(&b, "- %s\n", path)
	}
	return b.String()
}

func writeJSONValue(stdout io.Writer, payload any, stderr io.Writer) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	_, _ = fmt.Fprintln(stdout, string(encoded))
	return 0
}

func blockerItem(item map[string]any) operatorBlockerItem {
	return operatorBlockerItem{
		BlockerID:   firstStringField(item, "blocker_id", "id"),
		RunID:       stringField(item, "run_id"),
		JobID:       stringField(item, "job_id"),
		Severity:    stringField(item, "severity"),
		Kind:        firstStringField(item, "blocker_kind", "kind"),
		Description: truncateString(firstStringField(item, "description", "summary"), 160),
	}
}

func terminalRunState(state string) bool {
	switch state {
	case "completed", "failed", "canceled":
		return true
	default:
		return false
	}
}

func isDaemonUnreachable(err error) bool {
	var clientErr *rpcclient.Error
	return errors.As(err, &clientErr) && clientErr.Code == "daemon_unreachable"
}

func isDaemonAuthError(err error) bool {
	var clientErr *rpcclient.Error
	if !errors.As(err, &clientErr) {
		return false
	}
	switch clientErr.Code {
	case "token_unavailable", "token_missing", "token_malformed", "token_invalid", "token_revoked", "token_expired",
		"capability_missing", "capability_scope_mismatch", "capability_expired":
		return true
	default:
		return false
	}
}

func mapSlice(value any) []map[string]any {
	items := []map[string]any{}
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
	case []map[string]any:
		items = append(items, typed...)
	}
	return items
}

func stringField(m map[string]any, key string) string {
	return stringAny(m[key])
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringAny(m[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func boolAny(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func intAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func stringListAny(value any) []string {
	out := []string{}
	switch typed := value.(type) {
	case []string:
		out = append(out, typed...)
	case []any:
		for _, item := range typed {
			if s := stringAny(item); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func capStrings(values []string, limit int) []string {
	if limit < 0 {
		limit = 0
	}
	if len(values) <= limit {
		return append([]string(nil), values...)
	}
	return append([]string(nil), values[:limit]...)
}

func nonEmptyLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func dedupeStrings(values []string, limit int) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !shellSafeRune(r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellSafeRune(r rune) bool {
	switch {
	case r == '/', r == '.', r == '_', r == '-', r == ':', r == '+', r == '=':
		return true
	case r >= '0' && r <= '9':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= 'a' && r <= 'z':
		return true
	default:
		return false
	}
}

func truncateString(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func blank(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
