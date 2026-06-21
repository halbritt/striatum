package mutations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/agentloop"
	"github.com/halbritt/striatum/go/pkg/laneproviderauth"
)

// supervisedEnv builds the full environment for a supervised lane process.
//
// #87 (RFC 0096 §2): the lane env is constructed from an EXPLICIT ALLOWLIST,
// never from os.Environ(). Handing the lane the daemon's entire environment
// leaked any secret in the daemon env — a Postgres DSN, future cloud creds —
// into the lane and its visible pane (the live incident). supervisedEnvEntries
// already emits the explicit STRIATUM_* + PATH base with NO inheritance; this
// wrapper adds only the small, named pass-through set the adapters genuinely
// need (agent-loop MCP bootstrap vars + a handful of OS basics). Everything
// else — including every *DSN*/*POSTGRES*/PG*/DATABASE_URL var — is dropped.
//
// adapter is the bare CLI adapter name (agentloop.LaneAdapterName of the raw
// lane argv0, e.g. "claude"); it scopes per-adapter env hardening such as the
// #101 Claude Code welcome/update-nag suppression. It is the OriginalCommand
// adapter, not the agent-loop wrapper ("striatumd"), so the keys reach the real
// child CLI regardless of agent-loop wrapping.
func supervisedEnv(adapter, repoRoot, repositoryID, runID, sessionID, supervisorID, laneID, boundToken string) []string {
	entries := supervisedEnvEntries(adapter, repoRoot, repositoryID, runID, sessionID, supervisorID, laneID, boundToken)
	return mergeEnvReplacing(supervisedEnvPassThrough(os.Environ()), entries)
}

func supervisedLaneEnv(config supervisionStartConfig, supervisorID string) []string {
	adapter := config.adapterName()
	if strings.TrimSpace(config.RunAsUser) == "" {
		return normalizeSupervisedTerminalEnv(applyLaneLaunchEnv(config, supervisedEnv(adapter, config.RepoRoot, config.RepositoryID, config.RunID, config.SessionID, supervisorID, config.LaneID, config.CapabilityToken)))
	}
	entries := supervisedEnvEntries(adapter, config.RepoRoot, config.RepositoryID, config.RunID, config.SessionID, supervisorID, config.LaneID, config.CapabilityToken)
	base := supervisedRunAsPassThrough(os.Environ(), config.RunAsUser)
	if endpoint, err := agentloop.ResolveMCPEndpoint(config.RepoRoot, os.Environ()); err == nil && strings.TrimSpace(endpoint) != "" {
		base = mergeEnvReplacing(base, []string{"STRIATUM_MCP_URL=" + endpoint})
	}
	return normalizeSupervisedTerminalEnv(applyLaneLaunchEnv(config, mergeEnvReplacing(base, entries)))
}

func supervisedPTYHelperSpecEnv(config supervisionStartConfig, supervisorID string) []string {
	if strings.TrimSpace(config.RunAsUser) == "" {
		return normalizeSupervisedTerminalEnv(applyLaneLaunchEnv(config, supervisedEnvEntries(config.adapterName(), config.RepoRoot, config.RepositoryID, config.RunID, config.SessionID, supervisorID, config.LaneID, config.CapabilityToken)))
	}
	return supervisedLaneEnv(config, supervisorID)
}

func normalizeSupervisedTerminalEnv(env []string) []string {
	term := envValues(env)["TERM"]
	if strings.TrimSpace(term) != "" && strings.TrimSpace(term) != "dumb" {
		return env
	}
	return mergeEnvReplacing(env, []string{"TERM=xterm-256color"})
}

func providerAuthPreflightEnv(config supervisionStartConfig) []string {
	base := os.Environ()
	if strings.TrimSpace(config.RunAsUser) != "" {
		base = providerAuthRunAsBaseEnv(config.RunAsUser)
	}
	if len(config.LaunchEnv) > 0 {
		base = mergeEnvReplacing(base, commandEnvEntries(config.LaunchEnv))
	}
	return laneproviderauth.SanitizeEnv(base, config.LaunchPathPrefix)
}

func providerAuthRunAsBaseEnv(runAsUser string) []string {
	base := providerAuthRunAsPassThrough(os.Environ())
	updates := append([]string{"PATH=" + supervisedPath()}, laneUserIdentityEnv(runAsUser)...)
	return mergeEnvReplacing(base, updates)
}

func providerAuthRunAsPassThrough(base []string) []string {
	out := []string{}
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		switch key {
		case "TERM", "COLORTERM", "LANG", "LANGUAGE", "TZ":
			out = append(out, entry)
		default:
			if strings.HasPrefix(key, "LC_") {
				out = append(out, entry)
			}
		}
	}
	return out
}

// applyLaneLaunchEnv layers the workflow-authored lane launch env (#223) onto an
// already-built supervised lane env: command_env entries (which can never name
// PATH or a STRIATUM_-namespaced control var — enforced at parse) override the
// OS passthrough/provider base but lose to the daemon control entries, and
// path_prefix dirs are prepended to PATH. The result lets a workflow launch a
// lane (e.g. agy) with a controlled PATH and provider env without a
// machine-local wrapper, while the daemon's identity/token/endpoint vars stay
// authoritative.
func applyLaneLaunchEnv(config supervisionStartConfig, env []string) []string {
	if len(config.LaunchEnv) > 0 {
		env = mergeEnvReplacing(env, commandEnvEntries(config.LaunchEnv))
	}
	if len(config.LaunchPathPrefix) > 0 {
		env = prependLanePathPrefix(env, config.LaunchPathPrefix)
	}
	return env
}

func commandEnvEntries(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	sort.Strings(out)
	return out
}

func prependLanePathPrefix(env []string, prefix []string) []string {
	current := ""
	idx := -1
	for i, entry := range env {
		if key, value, ok := strings.Cut(entry, "="); ok && key == "PATH" {
			current = value
			idx = i
		}
	}
	seen := map[string]bool{}
	merged := make([]string, 0, len(prefix))
	for _, dir := range prefix {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		merged = append(merged, dir)
	}
	for _, dir := range filepath.SplitList(current) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		merged = append(merged, dir)
	}
	pathEntry := "PATH=" + strings.Join(merged, string(os.PathListSeparator))
	if idx >= 0 {
		out := append([]string(nil), env...)
		out[idx] = pathEntry
		return out
	}
	return append(env, pathEntry)
}

func rebridgeHelperEnv(supervisor supervisorControlRow) []string {
	runAsUser := supervisorRunAsUser(supervisor.Metadata)
	if runAsUser == "" {
		return []string{"PATH=" + supervisedPath()}
	}
	env := []string{"PATH=" + supervisedPath()}
	env = append(env, laneUserIdentityEnv(runAsUser)...)
	return env
}

func supervisedRunAsPassThrough(base []string, runAsUser string) []string {
	out := make([]string, 0, len(supervisedEnvAllowlistKeys))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		switch key {
		case "STRIATUM_MCP_URL",
			"STRIATUM_MCP_ADDR",
			"STRIATUM_MCP_PORT",
			"STRIATUM_DAEMON_MCP_HTTP_URL",
			"STRIATUM_DAEMON_MCP_HTTP_ENDPOINT_FILE",
			"STRIATUM_DAEMON_MCP_HTTP_ADDR",
			"STRIATUM_DAEMON_MCP_HTTP_PORT",
			"STRIATUM_DAEMON_RUNTIME_DIR",
			"STRIATUM_DAEMON_SOCKET",
			"TERM",
			"COLORTERM",
			"LANG",
			"LANGUAGE",
			"TZ":
			out = append(out, entry)
		default:
			if strings.HasPrefix(key, "LC_") {
				out = append(out, entry)
			}
		}
	}
	return mergeEnvReplacing(out, laneUserIdentityEnv(runAsUser))
}

func laneUserIdentityEnv(runAsUser string) []string {
	runAsUser = strings.TrimSpace(runAsUser)
	if runAsUser == "" {
		return nil
	}
	entries := []string{
		"USER=" + runAsUser,
		"LOGNAME=" + runAsUser,
	}
	if home := laneOSUserHome(runAsUser); home != "" {
		entries = append(entries, "HOME="+home)
	}
	return entries
}

var laneOSUserHome = func(name string) string {
	u, err := user.Lookup(name)
	if err != nil || strings.TrimSpace(u.HomeDir) == "" {
		return ""
	}
	return u.HomeDir
}

func configuredLaneRunAsUser() string {
	laneUser := strings.TrimSpace(os.Getenv(supervisedLaneOSUserEnv))
	if laneUser == "" {
		return ""
	}
	daemonUser := currentOSUsername()
	if daemonUser != "" && sameOSUsername(laneUser, daemonUser) {
		return ""
	}
	return laneUser
}

var currentOSUsername = func() string {
	if u, err := user.Current(); err == nil && strings.TrimSpace(u.Username) != "" {
		return strings.TrimSpace(u.Username)
	}
	return strings.TrimSpace(os.Getenv("USER"))
}

func sameOSUsername(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	return strings.HasSuffix(b, `\`+a)
}

func supervisedLaneCommandContext(ctx context.Context, command []string, workingDir, runAsUser string, env []string) *exec.Cmd {
	if strings.TrimSpace(runAsUser) == "" {
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Dir = workingDir
		cmd.Env = env
		return cmd
	}
	args := []string{"-n", "-u", strings.TrimSpace(runAsUser), "--", "env", "-i"}
	args = append(args, supervisedRunAsExecEnv(env)...)
	args = append(args, command...)
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.Dir = workingDir
	return cmd
}

func supervisedRunAsExecEnv(env []string) []string {
	seen := map[string]int{}
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			seen[key] = i
		}
	}
	out := make([]string, 0, len(seen))
	hasPath := false
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || seen[key] != i {
			continue
		}
		if key == "PATH" {
			hasPath = true
		}
		if sensitiveRunAsEnvKey(key) {
			continue
		}
		out = append(out, entry)
	}
	if !hasPath {
		out = append([]string{"PATH=" + supervisedPath()}, out...)
	}
	return out
}

func sensitiveRunAsEnvKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	if upper == "" {
		return true
	}
	switch upper {
	case "STRIATUM_MCP_TOKEN", "STRIATUM_MCP_TOKEN_FILE", "DATABASE_URL", "PGPASSWORD":
		return true
	}
	for _, marker := range []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "CREDENTIAL", "DSN"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}

func supervisedEnvEntries(adapter, repoRoot, repositoryID, runID, sessionID, supervisorID, laneID, boundToken string) []string {
	entries := []string{
		"PATH=" + supervisedPath(),
		"STRIATUM_REPOSITORY_ID=" + repositoryID,
		"STRIATUM_RUN_ID=" + runID,
		"STRIATUM_SESSION_ID=" + sessionID,
		"STRIATUM_SUPERVISOR_ID=" + supervisorID,
		"STRIATUM_REPO=" + repoRoot,
		"STRIATUM_LANE_ID=" + laneID,
	}
	if socketPath := supervisedDaemonSocketPath(os.Environ()); socketPath != "" {
		entries = append(entries, agentloop.EnvDaemonSocket+"="+socketPath)
	}
	// RFC 0096 V2 / #135: inject the lane's OWN session-bound capability token as
	// STRIATUM_MCP_TOKEN. It is set as an explicit entry (winning over any
	// inherited value via mergeEnvReplacing) and is the FIRST source
	// agentloop.ResolveTokenMaterial consults, so claude (inline --mcp-config),
	// codex (env), and agy (.gemini/settings.json) all authenticate with it. The
	// shared-token pass-through has been removed from supervisedEnvAllowlistKeys,
	// so when no bound token is provided the lane gets none (fails loudly) rather
	// than silently inheriting the daemon's operator override.
	if strings.TrimSpace(boundToken) != "" {
		entries = append(entries, "STRIATUM_MCP_TOKEN="+boundToken)
	}
	// #316: fold THIS daemon process run's MCP boot epoch into the lane env. The
	// lane's MCP client (agentloop.injectLaneMCPConfig) reads it from the env and
	// echoes it on every MCP request as the mcp.HeaderBootEpoch header; the
	// daemon rejects a request whose presented epoch differs from its live epoch
	// (a recycled-port hit — the MCP port is dynamic and the OS can rebind a
	// freed port to a DIFFERENT live daemon). Set as an explicit entry so it wins
	// over any inherited value. Empty (no epoch configured) injects nothing, and
	// an epoch-less request stays backward-compatible (the daemon allows it).
	if epoch := strings.TrimSpace(packageMCPBootEpoch); epoch != "" {
		entries = append(entries, agentloop.EnvMCPBootEpoch+"="+epoch)
	}
	return append(entries, supervisedAdapterEnvEntries(adapter)...)
}

func supervisedDaemonSocketPath(env []string) string {
	if socket := strings.TrimSpace(packageDaemonSocketPath); socket != "" {
		return socket
	}
	values := envValues(env)
	if socket := strings.TrimSpace(values[agentloop.EnvDaemonSocket]); socket != "" {
		return socket
	}
	if runtimeDir := strings.TrimSpace(values["STRIATUM_DAEMON_RUNTIME_DIR"]); runtimeDir != "" {
		return filepath.Join(runtimeDir, "daemon-go.sock")
	}
	if runtimeDir := strings.TrimSpace(values["XDG_RUNTIME_DIR"]); runtimeDir != "" {
		return filepath.Join(runtimeDir, "striatum", "daemon-go.sock")
	}
	return ""
}

func envValues(env []string) map[string]string {
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			values[key] = value
		}
	}
	return values
}

// supervisedAdapterEnvEntries returns the per-adapter, non-secret operational
// env knobs a supervised lane needs so its CLI acts on the work packet instead
// of parking on a startup splash. It is the env sibling of the per-adapter
// command/config hardening in agentloop.injectLaneMCPConfig (and of the agy
// usageStatisticsEnabled:false survey-suppression, #76).
//
// claude (#101 / RFC 0101 L2 lane-env hardening): a daemon-spawned Claude Code
// lane otherwise parks on the auto-updater "a new version is available" nag /
// onboarding splash and never acts on its packet — the single most common live
// dogfood wedge (the implement-lane stall behind #121). These are the
// authoritative env switches (Claude Code docs, code.claude.com/docs/en/env-vars,
// confirmed present in the installed claude 2.1.159 binary):
//
//   - DISABLE_AUTOUPDATER=1: disable the auto-updater + its "update available"
//     check; per the docs it takes precedence over the autoUpdates config.
//   - CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1: the bundle switch — equivalent
//     to setting DISABLE_AUTOUPDATER, DISABLE_FEEDBACK_COMMAND,
//     DISABLE_ERROR_REPORTING, and DISABLE_TELEMETRY together. We set it
//     alongside the explicit DISABLE_AUTOUPDATER so the update-nag stays
//     suppressed even if a future build narrows the bundle.
//
// Both keys are claude-namespaced / claude-read and harmless to other adapters,
// but we scope them per-adapter to keep the lane env minimal and the intent
// auditable. The first-run onboarding/theme splash is gated on the ~/.claude.json
// hasCompletedOnboarding config flag rather than an env var; we deliberately do
// NOT write the operator's ~/.claude.json (see report), so env covers the
// update-nag (the live #101 wedge) but not a never-onboarded profile.
func supervisedAdapterEnvEntries(adapter string) []string {
	switch adapter {
	case "claude":
		return []string{
			"DISABLE_AUTOUPDATER=1",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		}
	default:
		return nil
	}
}

// supervisedEnvAllowlistKeys are the EXACT daemon-env var names a supervised
// lane is allowed to inherit (#87 / RFC 0096 §2). The list is deliberately
// explicit and easy to audit: if a new var must reach a lane, add it here on
// purpose. Anything not named here — every DSN/Postgres/credential var — is
// dropped. PATH and the STRIATUM_REPOSITORY_ID/RUN_ID/SESSION_ID/SUPERVISOR_ID/
// REPO/LANE_ID vars are NOT listed here because supervisedEnvEntries always
// sets them freshly (overriding any inherited value via mergeEnvReplacing).
var supervisedEnvAllowlistKeys = map[string]bool{
	// Agent-loop MCP bootstrap (go/pkg/agentloop endpoint.go / token.go /
	// bootstrap.go AgentEnvironment): the `striatumd -agent-loop` subprocess
	// resolves the live MCP endpoint from its own environment, so the daemon must
	// pass the endpoint vars through for a lane to reach the control plane.
	//
	// RFC 0096 V2 / #135: STRIATUM_MCP_TOKEN and STRIATUM_MCP_TOKEN_FILE are
	// DELIBERATELY NOT allowlisted here. The lane must authenticate with its OWN
	// session-bound token (minted at supervise start and injected explicitly by
	// supervisedEnvEntries), never the daemon's shared operator-override token.
	// Allowlisting the shared token would let the per-session impersonation guard
	// be bypassed in live runs (the spoof would be treated as an honest operator
	// override). With both removed, the only token a lane can carry is the
	// injected bound one.
	"STRIATUM_MCP_URL":                       true,
	"STRIATUM_MCP_ADDR":                      true,
	"STRIATUM_MCP_PORT":                      true,
	"STRIATUM_DAEMON_MCP_HTTP_URL":           true,
	"STRIATUM_DAEMON_MCP_HTTP_ENDPOINT_FILE": true,
	"STRIATUM_DAEMON_MCP_HTTP_ADDR":          true,
	"STRIATUM_DAEMON_MCP_HTTP_PORT":          true,
	"STRIATUM_DAEMON_RUNTIME_DIR":            true,
	"STRIATUM_DAEMON_SOCKET":                 true,
	// Supervised-lane operational knobs (diagnostics + PATH augmentation), set
	// by the operator, not secrets.
	"STRIATUM_SUPERVISED_STDERR_LOG": true,
	"STRIATUM_SUPERVISED_PATH_DIRS":  true,
	"STRIATUM_SUPERVISOR_HELPER":     true,
	// OS basics every interactive CLI adapter (claude/codex/agy) needs.
	"HOME":            true,
	"USER":            true,
	"LOGNAME":         true,
	"TERM":            true,
	"COLORTERM":       true,
	"LANG":            true,
	"LANGUAGE":        true,
	"TZ":              true,
	"TMPDIR":          true,
	"XDG_RUNTIME_DIR": true,
	"XDG_CONFIG_HOME": true,
	"XDG_DATA_HOME":   true,
	"XDG_CACHE_HOME":  true,
	"SSH_AUTH_SOCK":   true, // git over ssh for lanes that push/fetch
}

// supervisedEnvPassThrough filters a base environment down to the explicit
// allowlist (#87). LC_* locale vars are matched by prefix because they are a
// small open family (LC_ALL, LC_CTYPE, LC_MESSAGES, …) that adapters expect for
// correct text handling; none of them carry secrets.
func supervisedEnvPassThrough(base []string) []string {
	out := make([]string, 0, len(supervisedEnvAllowlistKeys))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if supervisedEnvAllowlistKeys[key] || strings.HasPrefix(key, "LC_") {
			out = append(out, entry)
		}
	}
	return out
}

func mergeEnvReplacing(base []string, updates []string) []string {
	keys := map[string]bool{}
	for _, entry := range updates {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			keys[key] = true
		}
	}
	out := make([]string, 0, len(base)+len(updates))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" || keys[key] {
			continue
		}
		out = append(out, entry)
	}
	return append(out, updates...)
}

// openSupervisedStderr returns the stderr sink for a supervised lane: by
// default /dev/null (D028 no-capture), but if STRIATUM_SUPERVISED_STDERR_LOG
// is set, it's appended to that path. Used to surface agent-loop / lane
// failures that would otherwise be silent — debug only.
func openSupervisedStderr() (*os.File, error) {
	if path := strings.TrimSpace(os.Getenv("STRIATUM_SUPERVISED_STDERR_LOG")); path != "" {
		return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	}
	return os.OpenFile(os.DevNull, os.O_WRONLY, 0)
}

func supervisedPath() string {
	current := os.Getenv("PATH")
	entries := filepath.SplitList(current)
	seen := map[string]bool{}
	for _, entry := range entries {
		if entry != "" {
			seen[entry] = true
		}
	}
	for _, dir := range supervisedPathDirs() {
		if seen[dir] {
			continue
		}
		entries = append(entries, dir)
		seen[dir] = true
	}
	return strings.Join(entries, string(os.PathListSeparator))
}

func supervisedPathDirs() []string {
	rawDirs := []string{}
	if override := strings.TrimSpace(os.Getenv("STRIATUM_SUPERVISED_PATH_DIRS")); override != "" {
		rawDirs = filepath.SplitList(override)
	} else if home := supervisedHomeDir(); home != "" {
		rawDirs = []string{
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
		}
	}
	dirs := make([]string, 0, len(rawDirs))
	seen := map[string]bool{}
	for _, raw := range rawDirs {
		dir := filepath.Clean(strings.TrimSpace(raw))
		if dir == "." || dir == "" || !filepath.IsAbs(dir) || seen[dir] {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		dirs = append(dirs, dir)
		seen[dir] = true
	}
	return dirs
}

func supervisedHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return home
	}
	return strings.TrimSpace(os.Getenv("HOME"))
}

func resolveSupervisorHelper() (string, error) {
	if override := os.Getenv("STRIATUM_SUPERVISOR_HELPER"); override != "" {
		return override, nil
	}
	if found, err := exec.LookPath("striatum-supervisor-helper"); err == nil {
		return found, nil
	}
	repoHelper := filepath.Join("go", "bin", "striatum-supervisor-helper")
	if _, err := os.Stat(repoHelper); err == nil {
		abs, _ := filepath.Abs(repoHelper)
		return abs, nil
	}
	return "", fmt.Errorf("striatum-supervisor-helper not found; set STRIATUM_SUPERVISOR_HELPER or build go/bin/striatum-supervisor-helper")
}
