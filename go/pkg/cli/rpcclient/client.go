package rpcclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/halbritt/striatum/go/pkg/admin"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

const (
	EnvDaemonSocket    = "STRIATUM_DAEMON_SOCKET"
	EnvDaemonToken     = "STRIATUM_DAEMON_TOKEN"
	EnvDaemonTokenFile = "STRIATUM_DAEMON_TOKEN_FILE"
	DefaultDeadlineMS  = 30000

	// EnvDaemonRuntimeDir is the single-source-of-truth runtime directory for the
	// daemon. When the daemon runs as a system unit (e.g. striatumd.service) it sets
	// this to /run/striatum; interactive shells inherit it via /etc/profile.d/striatum.sh.
	// The CLI honours it so that non-login environments (scripts, lane helpers) that
	// miss the profile.d sourcing still resolve the correct socket path.
	EnvDaemonRuntimeDir = "STRIATUM_DAEMON_RUNTIME_DIR"

	// SystemDaemonSocket is the well-known socket path for the system-unit topology
	// (striatumd.service, socket nested in rpc/ for lane-ACL reasons — see service
	// file comment). Used as a last-resort fallback when no explicit socket or
	// runtime-dir env is configured and the user-scope socket does not exist, so the
	// CLI works out of the box against the system daemon without requiring the
	// operator to export STRIATUM_DAEMON_SOCKET manually.
	SystemDaemonSocket = "/run/striatum/rpc/daemon-go.sock"
)

type Config struct {
	SocketPath string
	Token      string
	TokenFile  string
	DeadlineMS int
}

type Client struct {
	Config Config
}

func ResolveConfig(env []string, socketPath string, token string, tokenFile string, deadlineMS int) (Config, error) {
	values := envMap(env)
	if socketPath == "" {
		socketPath = values[EnvDaemonSocket]
	}
	if socketPath == "" {
		socketPath = defaultSocketPath(values)
	}
	if token == "" {
		token = values[EnvDaemonToken]
	}
	if tokenFile == "" {
		tokenFile = values[EnvDaemonTokenFile]
	}
	if token == "" && tokenFile == "" {
		var err error
		tokenFile, err = admin.RuntimeTokenPath()
		if err != nil {
			return Config{}, err
		}
	}
	if deadlineMS == 0 {
		deadlineMS = DefaultDeadlineMS
	}
	return Config{SocketPath: socketPath, Token: token, TokenFile: tokenFile, DeadlineMS: deadlineMS}, nil
}

func (c Client) Invoke(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	tokens, err := c.resolveInvocationTokens()
	if err != nil {
		return nil, err
	}
	data, err := c.invokeWithToken(ctx, method, params, tokens.primary)
	if err == nil {
		return data, nil
	}
	if tokens.fallback == "" || tokens.fallback == tokens.primary || !isTokenAuthFailure(err) {
		return nil, err
	}
	data, fallbackErr := c.invokeWithToken(ctx, method, params, tokens.fallback)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	if tokens.repairTokenFile != "" {
		_ = admin.WriteRuntimeToken(tokens.repairTokenFile, tokens.fallback)
	}
	return data, nil
}

type invocationTokens struct {
	primary         string
	fallback        string
	repairTokenFile string
}

func (c Client) resolveInvocationTokens() (invocationTokens, error) {
	if c.Config.Token != "" {
		return invocationTokens{primary: c.Config.Token}, nil
	}
	if c.Config.TokenFile == "" {
		return invocationTokens{}, nil
	}
	fallback, _ := runtimeDiscoveryToken(c.Config.TokenFile)
	body, err := os.ReadFile(c.Config.TokenFile)
	if err != nil {
		if fallback != "" {
			return invocationTokens{primary: fallback, repairTokenFile: c.Config.TokenFile}, nil
		}
		return invocationTokens{}, &Error{Code: "token_unavailable", Message: fmt.Sprintf("read daemon capability token: %v", err), ExitCode: 11}
	}
	return invocationTokens{
		primary:         strings.TrimSpace(string(body)),
		fallback:        fallback,
		repairTokenFile: c.Config.TokenFile,
	}, nil
}

func (c Client) invokeWithToken(ctx context.Context, method string, params map[string]any, token string) (map[string]any, error) {
	conn, err := net.DialTimeout("unix", c.Config.SocketPath, 5*time.Second)
	if err != nil {
		return nil, &Error{Code: "daemon_unreachable", Message: fmt.Sprintf("daemon unreachable at %s: %v", c.Config.SocketPath, err), ExitCode: 11}
	}
	defer func() { _ = conn.Close() }()

	reader := bufio.NewReader(conn)
	if _, err := send(ctx, conn, reader, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     requestID("hello"),
		Method:        "daemon.hello",
		Params: map[string]any{"client": map[string]any{
			"name":               "striatum-go-cli",
			"supported_envelope": []int{rpc.SupportedEnvelopeVersion},
			"supported_framings": []string{rpc.DefaultFraming},
		}},
		DeadlineMS: c.Config.DeadlineMS,
	}); err != nil {
		return nil, err
	}
	response, err := send(ctx, conn, reader, rpc.Envelope{
		SchemaVersion:   rpc.SupportedEnvelopeVersion,
		RequestID:       requestID("cli"),
		Method:          method,
		Params:          params,
		CapabilityToken: token,
		DeadlineMS:      c.Config.DeadlineMS,
	})
	if err != nil {
		return nil, err
	}
	return response.Data, nil
}

func runtimeDiscoveryToken(tokenFile string) (string, bool) {
	if filepath.Base(tokenFile) != "client-token" {
		return "", false
	}
	discoveryPath := filepath.Join(filepath.Dir(tokenFile), "discovery.json")
	info, err := os.Stat(discoveryPath)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		return "", false
	}
	body, err := os.ReadFile(discoveryPath)
	if err != nil {
		return "", false
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return "", false
	}
	token, _ := data["client_token"].(string)
	token = strings.TrimSpace(token)
	return token, token != ""
}

func isTokenAuthFailure(err error) bool {
	var clientErr *Error
	if errors.As(err, &clientErr) {
		switch clientErr.Code {
		case "token_missing", "token_malformed", "token_invalid", "token_revoked", "token_expired",
			"capability_missing", "capability_scope_mismatch", "capability_expired":
			return true
		}
	}
	return false
}

func send(ctx context.Context, conn net.Conn, reader *bufio.Reader, envelope rpc.Envelope) (rpc.Response, error) {
	// Apply a read/write deadline so a daemon that accepts the connection then
	// wedges before responding cannot hang reader.ReadBytes forever. The 5s
	// DialTimeout only covers connect; the CLI invokes with context.Background()
	// (cmd/striatum/main.go), so without this every verb would block indefinitely
	// on a wedged-after-accept daemon. A context deadline (when present) wins;
	// otherwise fall back to the envelope's deadline_ms — the same budget the
	// daemon is told to honour — so the client and daemon share one timeout (#358).
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else if envelope.DeadlineMS > 0 {
		_ = conn.SetDeadline(time.Now().Add(time.Duration(envelope.DeadlineMS) * time.Millisecond))
	}
	encoded, err := envelope.Encode()
	if err != nil {
		return rpc.Response{}, err
	}
	if _, err := conn.Write(append(encoded, '\n')); err != nil {
		return rpc.Response{}, &Error{Code: "daemon_unreachable", Message: fmt.Sprintf("write daemon RPC request: %v", err), ExitCode: 11}
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		return rpc.Response{}, &Error{Code: "daemon_unreachable", Message: fmt.Sprintf("read daemon RPC response: %v", err), ExitCode: 11}
	}
	response, err := rpc.DecodeResponse(line)
	if err != nil {
		return rpc.Response{}, &Error{Code: "schema_invalid", Message: err.Error(), ExitCode: 10}
	}
	if !response.OK {
		return rpc.Response{}, rpcError(response)
	}
	return response, nil
}

type Error struct {
	Code    string
	Message string
	// Suggestion is the in-band remediation the RFC 0111 error catalog fills on
	// the daemon side (rpc.Error.Suggestion). Threading it through to the client
	// is the whole point of the catalog: the agent-driven consumer sees the exact
	// remedy, not just the failure message (#358).
	Suggestion string
	ExitCode   int
}

func (e *Error) Error() string {
	return e.Message
}

func rpcError(response rpc.Response) error {
	code, _ := response.Data["code"].(string)
	message, _ := response.Data["message"].(string)
	suggestion, _ := response.Data["suggestion"].(string)
	if code == "" {
		code = "daemon_error"
	}
	if message == "" {
		message = code
	}
	return &Error{Code: code, Message: message, Suggestion: suggestion, ExitCode: exitCode(code)}
}

func ExitCode(err error) int {
	var clientErr *Error
	if errors.As(err, &clientErr) && clientErr.ExitCode != 0 {
		return clientErr.ExitCode
	}
	var rpcErr *rpc.Error
	if errors.As(err, &rpcErr) {
		return exitCode(rpcErr.Code)
	}
	return 1
}

func exitCode(code string) int {
	switch code {
	case "artifact_error", "write_scope_drift":
		return 6
	case "version_incompatible":
		return 10
	case "daemon_unreachable":
		return 11
	case "repo_not_registered":
		return 12
	case "token_missing", "token_malformed", "token_invalid", "token_revoked", "token_expired", "capability_missing", "capability_scope_mismatch", "capability_expired":
		return 13
	case "schema_invalid", "method_unknown":
		return 2
	default:
		return 1
	}
}

func requestID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func envMap(env []string) map[string]string {
	values := map[string]string{}
	if env == nil {
		env = os.Environ()
	}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values
}

// defaultSocketPath resolves the daemon socket path when neither an explicit
// flag value nor STRIATUM_DAEMON_SOCKET env is set. Precedence:
//
//  1. STRIATUM_DAEMON_RUNTIME_DIR → <dir>/daemon-go.sock   (system-unit topology)
//  2. XDG_RUNTIME_DIR             → <dir>/striatum/daemon-go.sock (user-scope topology)
//  3. system socket fallback       → SystemDaemonSocket, when present on disk
//     (handles non-login shells that miss /etc/profile.d/striatum.sh but the
//     system daemon is running)
//  4. XDG_RUNTIME_DIR default     → original user-scope path (legible error)
func defaultSocketPath(values map[string]string) string {
	// Tier 1: explicit runtime-dir env (set by the system unit and by profile.d).
	// Mirror the logic in striatumd/main.go::defaultSocketPath so the CLI and
	// daemon agree on where the socket lives when STRIATUM_DAEMON_RUNTIME_DIR is
	// set. The system unit nests the RPC socket one level deep (rpc/) for
	// lane-ACL reasons (see striatumd.service), but the daemon writes the actual
	// path directly via the -socket flag. We look for daemon-go.sock directly
	// inside the runtime dir first (rpc/daemon-go.sock), then bare.
	if runtimeDir := strings.TrimSpace(values[EnvDaemonRuntimeDir]); runtimeDir != "" {
		// The system unit puts the socket at <runtimeDir>/rpc/daemon-go.sock;
		// a future or alternate topology might put it at <runtimeDir>/daemon-go.sock.
		// Try the rpc/ sub-path first (matching the production layout), then bare.
		rpcSocket := filepath.Join(runtimeDir, "rpc", "daemon-go.sock")
		if _, err := os.Stat(rpcSocket); err == nil {
			return rpcSocket
		}
		return filepath.Join(runtimeDir, "daemon-go.sock")
	}

	// Tier 2: user-scope socket derived from XDG_RUNTIME_DIR (user-unit or
	// development topology).
	userSocket := ""
	if xdg := values["XDG_RUNTIME_DIR"]; xdg != "" {
		userSocket = filepath.Join(xdg, "striatum", "daemon-go.sock")
	} else {
		userSocket = filepath.Join(os.TempDir(), "striatum", "daemon-go.sock")
	}
	if _, err := os.Stat(userSocket); err == nil {
		return userSocket
	}

	// Tier 3: system socket fallback. When no env pins the location but the
	// well-known system socket exists on disk, prefer it. This recovers
	// non-login shells (scripts, agent lanes, sudo -i) that did not source
	// /etc/profile.d/striatum.sh. The user socket (tier 2) above would have
	// been returned already if it existed, so reaching here means it is absent.
	if _, err := os.Stat(SystemDaemonSocket); err == nil {
		return SystemDaemonSocket
	}

	// Tier 4: return the user-scope path as the default even if it does not
	// exist — the caller gets a legible daemon_unreachable error naming the
	// path it tried, rather than a confusing "no such file" stat error.
	return userSocket
}
