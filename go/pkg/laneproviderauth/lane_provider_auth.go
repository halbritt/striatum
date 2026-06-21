package laneproviderauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	ProviderCodex = "codex"

	StatusPassed = "passed"
	StatusFailed = "failed"

	ProbeCodexOutputLastMessage = "codex_exec_output_last_message"
	ProbeCodexOfflineAuthFile   = "codex_offline_auth_file"

	SuccessSignalMatched  = "matched"
	SuccessSignalMissing  = "missing"
	SuccessSignalEmpty    = "empty"
	SuccessSignalMismatch = "mismatch"
	SuccessSignalNotRun   = "not_run"

	// CodexAuthFileName is the credential file codex writes under $CODEX_HOME
	// (default ~/.codex). Its presence + basic JSON validity is the cheap,
	// OFFLINE preflight: a valid lane login leaves a parseable auth.json with a
	// recognizable credential field, and no billed model turn is required to
	// confirm the lane can authenticate. #556.
	CodexAuthFileName = "auth.json"

	FailureAuthFailed       = "lane_provider_auth_failed"
	FailureBinaryMissing    = "lane_provider_binary_missing"
	FailureUnavailable      = "lane_provider_unavailable"
	FailureTimeout          = "lane_provider_preflight_timeout"
	FailureLaunchFailed     = "lane_provider_preflight_launch_failed"
	FailureUnsupported      = "lane_provider_preflight_unsupported"
	FailureUnexpectedResult = "lane_provider_preflight_unexpected_result"

	DefaultTimeout = 45 * time.Second
)

const (
	GateAuto     GateMode = "auto"
	GateRequired GateMode = "required"
	GateOff      GateMode = "off"
)

type GateMode string

func ParseGateMode(value string) (GateMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(GateAuto):
		return GateAuto, nil
	case string(GateRequired):
		return GateRequired, nil
	case string(GateOff):
		return GateOff, nil
	default:
		return "", fmt.Errorf("provider_auth_gate must be one of auto, required, off")
	}
}

func IsFailureClass(value string) bool {
	switch strings.TrimSpace(value) {
	case FailureAuthFailed,
		FailureBinaryMissing,
		FailureUnavailable,
		FailureTimeout,
		FailureLaunchFailed,
		FailureUnsupported,
		FailureUnexpectedResult:
		return true
	default:
		return false
	}
}

type Params struct {
	Provider  string
	Binary    string
	RunID     string
	LaneID    string
	RunAsUser string
	Env       []string
	Timeout   time.Duration
	Runner    Runner
}

type CommandSpec struct {
	Name string
	Args []string
	Dir  string
	Env  []string
}

type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
	TimedOut bool
}

type Runner interface {
	Run(context.Context, CommandSpec) CommandResult
}

type RunnerFunc func(context.Context, CommandSpec) CommandResult

func (f RunnerFunc) Run(ctx context.Context, spec CommandSpec) CommandResult {
	return f(ctx, spec)
}

type Result struct {
	Checked           bool
	Provider          string
	RunID             string
	LaneID            string
	RunAsUser         string
	Status            string
	FailureClass      string
	Probe             string
	ExitCode          int
	StdoutBytes       int
	StderrBytes       int
	SuccessSignal     string
	RawOutputReturned bool
	Network           string
	Costing           string
	DurationMS        int64
	Remediation       string
}

func (r Result) Passed() bool {
	return r.Status == StatusPassed && r.FailureClass == ""
}

func (r Result) ToMap() map[string]any {
	out := map[string]any{
		"checked":             r.Checked,
		"provider":            r.Provider,
		"status":              r.Status,
		"failure_class":       nil,
		"probe":               r.Probe,
		"exit_code":           r.ExitCode,
		"stdout_bytes":        r.StdoutBytes,
		"stderr_bytes":        r.StderrBytes,
		"success_signal":      r.SuccessSignal,
		"raw_output_returned": false,
		"network":             r.Network,
		"costing":             r.Costing,
		"duration_ms":         r.DurationMS,
		"remediation":         r.Remediation,
	}
	if r.RunID != "" {
		out["run_id"] = r.RunID
	}
	if r.LaneID != "" {
		out["lane_id"] = r.LaneID
	}
	if r.RunAsUser != "" {
		out["run_as_user"] = r.RunAsUser
	}
	if r.FailureClass != "" {
		out["failure_class"] = r.FailureClass
	}
	return out
}

func Check(ctx context.Context, params Params) Result {
	provider := strings.ToLower(strings.TrimSpace(params.Provider))
	if provider == "" {
		provider = ProviderCodex
	}
	started := time.Now()
	result := baseResult(params, provider)
	if provider != ProviderCodex {
		result.Checked = false
		result.Status = StatusFailed
		result.FailureClass = FailureUnsupported
		result.Remediation = remediation(FailureUnsupported)
		return result
	}
	timeout := params.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runner := params.Runner
	if runner == nil {
		runner = ExecRunner{}
	}

	// #556: prefer a cheap OFFLINE check over the billed model round-trip. A
	// valid lane login leaves a parseable $CODEX_HOME/auth.json with a
	// recognizable credential field; confirming that is sufficient to let the
	// lane launch (the lane's own codex process re-validates/refreshes on first
	// use). The offline check runs FOR THE LANE USER's HOME — the preflight env
	// carries the lane user's HOME/CODEX_HOME — so it inspects the lane's
	// auth.json, not the daemon user's. Only when the auth artifact is missing or
	// unparseable do we fall through to the (billed) live smoke, which still
	// distinguishes a genuine auth failure from a transient/unavailable one.
	offline := offlineAuthProbe(ctx, runner, params)
	if offline.attempted {
		result.Probe = ProbeCodexOfflineAuthFile
		result.Network = "no_network_offline_auth_file"
		result.Costing = "no_provider_tokens_spent"
		if offline.valid {
			result.Status = StatusPassed
			result.FailureClass = ""
			result.SuccessSignal = SuccessSignalMatched
			result.Remediation = "none"
			result.DurationMS = elapsedMS(started)
			return result
		}
		if offline.definitivelyMissing {
			// The lane has no usable codex credential at all — a genuine auth gap
			// the operator must fix (codex login as the lane user). This keeps the
			// gate non-vacuous: a rotten/absent auth.json still refuses.
			result = failed(result, FailureAuthFailed)
			result.SuccessSignal = SuccessSignalMissing
			result.DurationMS = elapsedMS(started)
			return result
		}
		// offline.attempted && !valid && !definitivelyMissing: the probe could
		// not reach a verdict (e.g. could not read the file for a reason other
		// than absence). Fall through to the live smoke below rather than guess.
		result = baseResult(params, provider)
	}

	tmpDir, err := os.MkdirTemp("", "striatum-lane-provider-auth-*")
	if err != nil {
		result.Status = StatusFailed
		result.FailureClass = FailureLaunchFailed
		result.Remediation = remediation(FailureLaunchFailed)
		result.DurationMS = elapsedMS(started)
		return result
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outputPath := filepath.Join(tmpDir, "last-message.txt")
	command := BuildCodexSmokeCommand(CodexSmokeOptions{
		Binary:     params.Binary,
		CWD:        tmpDir,
		OutputPath: outputPath,
	})
	spec := BuildLaunchSpec(command, tmpDir, params.RunAsUser, SanitizeEnv(params.Env, nil))

	key := SerializationKey(provider, params.RunAsUser, ResolveAuthHome(provider, specEnvForKey(spec, params.Env)))
	unlock := lockKey(key)
	defer unlock()

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	runResult := runner.Run(runCtx, spec)
	if runCtx.Err() == context.DeadlineExceeded {
		runResult.TimedOut = true
	}
	successSignal, readErr := readSuccessSignal(outputPath)
	result = classifyCodexResult(result, runResult, successSignal, readErr)
	result.DurationMS = elapsedMS(started)
	return result
}

type CodexSmokeOptions struct {
	Binary     string
	CWD        string
	OutputPath string
}

func BuildCodexSmokeCommand(options CodexSmokeOptions) []string {
	binary := strings.TrimSpace(options.Binary)
	if binary == "" {
		binary = ProviderCodex
	}
	return []string{
		binary,
		"exec",
		"--ignore-user-config",
		"--ignore-rules",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"-c", `approval_policy="never"`,
		"-C", options.CWD,
		"--output-last-message", options.OutputPath,
		"--json",
		"Reply exactly: ok",
	}
}

func BuildLaunchSpec(command []string, workingDir, runAsUser string, env []string) CommandSpec {
	if len(command) == 0 {
		return CommandSpec{Dir: workingDir, Env: uniqueLastEnv(env)}
	}
	env = ensurePath(uniqueLastEnv(env))
	if strings.TrimSpace(runAsUser) == "" {
		return CommandSpec{
			Name: command[0],
			Args: append([]string(nil), command[1:]...),
			Dir:  workingDir,
			Env:  env,
		}
	}
	args := []string{"-n", "-u", strings.TrimSpace(runAsUser), "--", "env", "-i"}
	args = append(args, env...)
	args = append(args, command...)
	return CommandSpec{Name: "sudo", Args: args, Dir: workingDir}
}

func SanitizeEnv(base []string, pathPrefix []string) []string {
	out := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		if providerEnvAllowlist[key] || strings.HasPrefix(key, "LC_") {
			out = append(out, entry)
		}
	}
	out = uniqueLastEnv(out)
	if len(pathPrefix) > 0 {
		out = prependPathPrefix(out, pathPrefix)
	}
	return ensurePath(out)
}

var providerEnvAllowlist = map[string]bool{
	"COLORTERM":       true,
	"CODEX_HOME":      true,
	"HOME":            true,
	"LANG":            true,
	"LANGUAGE":        true,
	"LOGNAME":         true,
	"PATH":            true,
	"SSH_AUTH_SOCK":   true,
	"TERM":            true,
	"TMPDIR":          true,
	"TZ":              true,
	"USER":            true,
	"XDG_CACHE_HOME":  true,
	"XDG_CONFIG_HOME": true,
	"XDG_DATA_HOME":   true,
}

func ResolveAuthHome(provider string, env []string) string {
	values := envValues(env)
	if strings.ToLower(strings.TrimSpace(provider)) == ProviderCodex {
		if home := strings.TrimSpace(values["CODEX_HOME"]); home != "" {
			return filepath.Clean(home)
		}
	}
	if home := strings.TrimSpace(values["HOME"]); home != "" {
		if strings.ToLower(strings.TrimSpace(provider)) == ProviderCodex {
			return filepath.Join(filepath.Clean(home), ".codex")
		}
		return filepath.Clean(home)
	}
	return "unknown"
}

func SerializationKey(provider, runAsUser, authHome string) string {
	return strings.ToLower(strings.TrimSpace(provider)) + "\x00" +
		strings.TrimSpace(runAsUser) + "\x00" +
		filepath.Clean(strings.TrimSpace(authHome))
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, spec CommandSpec) CommandResult {
	if strings.TrimSpace(spec.Name) == "" {
		return CommandResult{ExitCode: -1, Err: errors.New("empty command")}
	}
	cmd := exec.CommandContext(ctx, spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{
		ExitCode: 0,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Err:      err,
	}
	if err != nil {
		result.ExitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
	}
	return result
}

func baseResult(params Params, provider string) Result {
	probe := ""
	if provider == ProviderCodex {
		probe = ProbeCodexOutputLastMessage
	}
	return Result{
		Checked:           true,
		Provider:          provider,
		RunID:             params.RunID,
		LaneID:            params.LaneID,
		RunAsUser:         strings.TrimSpace(params.RunAsUser),
		Status:            StatusPassed,
		Probe:             probe,
		ExitCode:          0,
		SuccessSignal:     SuccessSignalNotRun,
		RawOutputReturned: false,
		Network:           "provider_cli_may_use_network",
		Costing:           "provider_tokens_may_be_spent",
		Remediation:       "none",
	}
}

func classifyCodexResult(base Result, run CommandResult, successSignal string, readErr error) Result {
	base.ExitCode = run.ExitCode
	base.StdoutBytes = len(run.Stdout)
	base.StderrBytes = len(run.Stderr)
	base.SuccessSignal = successSignalState(successSignal, readErr)
	if run.TimedOut {
		return failed(base, FailureTimeout)
	}
	if run.Err != nil && run.ExitCode < 0 {
		if isMissingBinaryError(run.Err, run.Stderr) {
			return failed(base, FailureBinaryMissing)
		}
		return failed(base, FailureLaunchFailed)
	}
	if run.ExitCode != 0 {
		text := strings.ToLower(run.Stdout + "\n" + run.Stderr + "\n" + errorString(run.Err))
		switch {
		case textSuggestsMissingBinary(text):
			return failed(base, FailureBinaryMissing)
		case textSuggestsAuthFailure(text):
			return failed(base, FailureAuthFailed)
		case textSuggestsUnavailable(text):
			return failed(base, FailureUnavailable)
		default:
			// #556: a nonzero exit that does NOT name an auth problem must NOT be
			// classified as an auth failure. A read-only-sandbox network block,
			// provider quota, or a codex flag/version drift (e.g. codex 0.140 new
			// since the deploy) all exit nonzero while the lane's auth is valid;
			// mislabeling those as lane_provider_auth_failed refused every codex
			// lane and crash-looped the driver to death. Treat an unrecognized
			// nonzero exit as UNAVAILABLE (a retryable/inspect class), not auth.
			return failed(base, FailureUnavailable)
		}
	}
	return base
}

// codexOfflineAuthOutcome reports the result of the offline auth.json probe.
//   - attempted: the offline probe ran (an auth home was resolvable).
//   - valid: auth.json was present and parsed as JSON carrying a recognizable
//     credential field.
//   - definitivelyMissing: auth.json was absent (or empty/zero-length), i.e. a
//     genuine "no credential" gap.
//
// attempted && !valid && !definitivelyMissing means the probe could not reach a
// verdict (e.g. an unexpected read error) and the caller should fall through to
// the live smoke rather than assume an auth failure.
type codexOfflineAuthOutcome struct {
	attempted           bool
	valid               bool
	definitivelyMissing bool
}

// offlineAuthProbe is the seam the offline auth check runs through; it is a var
// so the live-smoke classifier tests can bypass the offline short-circuit and
// drive classifyCodexResult end-to-end through Check. Production always uses
// checkCodexOfflineAuth.
var offlineAuthProbe = checkCodexOfflineAuth

// checkCodexOfflineAuth inspects $CODEX_HOME/auth.json for the lane user without
// spending a provider token (#556). When the preflight targets a run-as lane
// user the daemon process may not be able to read the lane-owned auth.json
// directly, so the read is delegated through the same `sudo -n -u <user> env -i`
// launch shape the live smoke uses; with no run-as user it reads in-process.
func checkCodexOfflineAuth(ctx context.Context, runner Runner, params Params) codexOfflineAuthOutcome {
	env := SanitizeEnv(params.Env, nil)
	authHome := ResolveAuthHome(ProviderCodex, env)
	if authHome == "" || authHome == "unknown" {
		return codexOfflineAuthOutcome{}
	}
	authPath := filepath.Join(authHome, CodexAuthFileName)

	runAsUser := strings.TrimSpace(params.RunAsUser)
	if runAsUser == "" {
		payload, err := os.ReadFile(authPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return codexOfflineAuthOutcome{attempted: true, definitivelyMissing: true}
			}
			// Could not read for some other reason (permissions, etc.) — let the
			// live smoke decide.
			return codexOfflineAuthOutcome{attempted: true}
		}
		return classifyOfflineAuthPayload(payload)
	}

	// Delegate the read to the lane user. `cat` of an absent file exits nonzero
	// with a "no such file" stderr (treated as definitively missing); a readable
	// file is validated from its stdout payload.
	command := []string{"cat", "--", authPath}
	spec := BuildLaunchSpec(command, "", runAsUser, env)
	runCtx, cancel := context.WithTimeout(ctx, offlineAuthTimeout())
	defer cancel()
	res := runner.Run(runCtx, spec)
	if res.ExitCode == 0 && res.Err == nil {
		return classifyOfflineAuthPayload([]byte(res.Stdout))
	}
	text := strings.ToLower(res.Stderr + "\n" + errorString(res.Err))
	if strings.Contains(text, "no such file") || strings.Contains(text, "not found") {
		return codexOfflineAuthOutcome{attempted: true, definitivelyMissing: true}
	}
	// Ambiguous failure (sudo policy, timeout, unexpected) — fall through to the
	// live smoke rather than asserting an auth failure.
	return codexOfflineAuthOutcome{attempted: true}
}

func offlineAuthTimeout() time.Duration {
	return 5 * time.Second
}

// classifyOfflineAuthPayload validates an auth.json payload: it must parse as a
// JSON object and carry at least one recognizable credential field. A truly
// empty payload is "definitively missing"; a present-but-unparseable / fieldless
// payload is a rotten credential (definitively missing → genuine auth gap), so
// the gate is never vacuous.
func classifyOfflineAuthPayload(payload []byte) codexOfflineAuthOutcome {
	if len(bytes.TrimSpace(payload)) == 0 {
		return codexOfflineAuthOutcome{attempted: true, definitivelyMissing: true}
	}
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		// Present but not valid JSON — a corrupt credential. Treat as a genuine
		// auth gap so the gate still refuses.
		return codexOfflineAuthOutcome{attempted: true, definitivelyMissing: true}
	}
	if offlineAuthHasCredential(doc) {
		return codexOfflineAuthOutcome{attempted: true, valid: true}
	}
	// Parseable JSON but no recognizable credential field — rotten auth.json.
	return codexOfflineAuthOutcome{attempted: true, definitivelyMissing: true}
}

// offlineAuthHasCredential reports whether a parsed auth.json carries a
// recognizable credential. codex writes either an OPENAI_API_KEY-style flat key
// or an OAuth `tokens` object (id/access/refresh + account_id); we accept any of
// those shapes (and a couple of historical key names) so a valid login of either
// kind passes while an empty/fieldless object fails.
func offlineAuthHasCredential(doc map[string]any) bool {
	for _, key := range []string{"OPENAI_API_KEY", "openai_api_key", "api_key", "apiKey"} {
		if v, ok := doc[key]; ok && nonEmptyString(v) {
			return true
		}
	}
	if tokens, ok := doc["tokens"].(map[string]any); ok {
		for _, key := range []string{"access_token", "id_token", "refresh_token"} {
			if v, ok := tokens[key]; ok && nonEmptyString(v) {
				return true
			}
		}
	}
	for _, key := range []string{"access_token", "id_token", "refresh_token", "token"} {
		if v, ok := doc[key]; ok && nonEmptyString(v) {
			return true
		}
	}
	return false
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) != ""
}

func successSignalState(successSignal string, readErr error) string {
	if readErr != nil {
		return SuccessSignalMissing
	}
	if strings.TrimSpace(successSignal) == "ok" {
		return SuccessSignalMatched
	}
	if strings.TrimSpace(successSignal) == "" {
		return SuccessSignalEmpty
	}
	return SuccessSignalMismatch
}

func failed(base Result, failureClass string) Result {
	base.Status = StatusFailed
	base.FailureClass = failureClass
	base.Remediation = remediation(failureClass)
	return base
}

func remediation(failureClass string) string {
	switch failureClass {
	case FailureAuthFailed:
		return "refresh the provider login for the lane OS user, then retry supervise.start"
	case FailureBinaryMissing:
		return "install the provider CLI for the lane launch environment or add the binary directory to lane path_prefix"
	case FailureUnavailable:
		return "retry after provider or network availability recovers"
	case FailureTimeout:
		return "inspect the lane provider login for an interactive prompt or hung refresh path"
	case FailureLaunchFailed:
		return "fix lane run-as user, sudo, home directory, and launch environment provisioning"
	case FailureUnsupported:
		return "use --provider-auth-gate off or configure a provider with a supported auth preflight"
	case FailureUnexpectedResult:
		return "inspect the provider CLI manually; the smoke completed with an unsupported result shape"
	default:
		return "inspect the lane provider auth preflight result"
	}
}

func isMissingBinaryError(err error, stderr string) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || textSuggestsMissingBinary(strings.ToLower(stderr+"\n"+err.Error()))
}

func textSuggestsMissingBinary(text string) bool {
	for _, needle := range []string{
		"executable file not found",
		"command not found",
		"no such file or directory",
		"not found",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func textSuggestsUnavailable(text string) bool {
	for _, needle := range []string{
		"network",
		"service unavailable",
		"temporarily unavailable",
		"connection refused",
		"connection reset",
		"dns",
		"rate limit",
		"too many requests",
		"429",
		"500",
		"502",
		"503",
		"504",
		"proxy",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func textSuggestsAuthFailure(text string) bool {
	for _, needle := range []string{
		"auth",
		"authentication",
		"unauthorized",
		"not logged in",
		"login",
		"sign in",
		"signin",
		"credential",
		"credentials",
		"expired",
		"revoked",
		"oauth",
		"api key",
		"token",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func readSuccessSignal(path string) (string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(payload) > 4096 {
		payload = payload[:4096]
	}
	return string(payload), nil
}

func elapsedMS(start time.Time) int64 {
	ms := time.Since(start).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var serializationLocks sync.Map

func lockKey(key string) func() {
	value, _ := serializationLocks.LoadOrStore(key, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func specEnvForKey(spec CommandSpec, fallback []string) []string {
	if len(spec.Env) > 0 {
		return spec.Env
	}
	for i, arg := range spec.Args {
		if arg == "env" && i+1 < len(spec.Args) && spec.Args[i+1] == "-i" {
			env := []string{}
			for _, entry := range spec.Args[i+2:] {
				if !strings.Contains(entry, "=") {
					break
				}
				env = append(env, entry)
			}
			return env
		}
	}
	return fallback
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

func uniqueLastEnv(env []string) []string {
	last := map[string]int{}
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			last[key] = i
		}
	}
	out := make([]string, 0, len(last))
	for i, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key != "" && last[key] == i {
			out = append(out, entry)
		}
	}
	return out
}

func ensurePath(env []string) []string {
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			return env
		}
	}
	return append([]string{"PATH=" + os.Getenv("PATH")}, env...)
}

func prependPathPrefix(env []string, prefix []string) []string {
	values := envValues(env)
	current := values["PATH"]
	seen := map[string]bool{}
	parts := []string{}
	for _, dir := range prefix {
		dir = strings.TrimSpace(dir)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	for _, dir := range filepath.SplitList(current) {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		parts = append(parts, dir)
	}
	out := make([]string, 0, len(env))
	replaced := false
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && key == "PATH" {
			out = append(out, "PATH="+strings.Join(parts, string(os.PathListSeparator)))
			replaced = true
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append([]string{"PATH=" + strings.Join(parts, string(os.PathListSeparator))}, out...)
	}
	return out
}
