package rpcclient

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

// TestDefaultSocketPathPrecedence covers the four-tier fallback introduced in
// fix(cli) #541: explicit env > STRIATUM_DAEMON_RUNTIME_DIR > user socket (if
// present) > system socket fallback (if present) > user socket default.
//
// The test uses only temp-dir sockets so it works on any host regardless of
// whether /run/striatum/rpc/daemon-go.sock actually exists.
func TestDefaultSocketPathPrecedence(t *testing.T) {
	t.Run("ExplicitEnvSocketWins", func(t *testing.T) {
		// When STRIATUM_DAEMON_SOCKET is set, ResolveConfig returns it verbatim
		// regardless of any runtime-dir or XDG env.
		cfg, err := ResolveConfig([]string{
			"STRIATUM_DAEMON_SOCKET=/explicit/sock",
			"STRIATUM_DAEMON_RUNTIME_DIR=/should/be/ignored",
			"XDG_RUNTIME_DIR=/also/ignored",
		}, "", "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.SocketPath != "/explicit/sock" {
			t.Fatalf("socket = %q, want /explicit/sock", cfg.SocketPath)
		}
	})

	t.Run("RuntimeDirEnvSocketPath", func(t *testing.T) {
		// When STRIATUM_DAEMON_RUNTIME_DIR is set and the rpc/ sub-socket exists,
		// the CLI picks it up — matching the production system-unit layout.
		tmp := t.TempDir()
		rpcDir := filepath.Join(tmp, "rpc")
		if err := os.MkdirAll(rpcDir, 0o700); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(rpcDir, "daemon-go.sock")
		if err := os.WriteFile(sock, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := defaultSocketPath(map[string]string{
			"STRIATUM_DAEMON_RUNTIME_DIR": tmp,
		})
		if got != sock {
			t.Fatalf("socket = %q, want %q", got, sock)
		}
	})

	t.Run("UserSocketPresentPreferredOverSystemFallback", func(t *testing.T) {
		// When XDG_RUNTIME_DIR points to a dir where the user-scope socket exists,
		// it is returned without probing the system socket. This preserves the
		// user-unit / development-machine topology.
		tmp := t.TempDir()
		userSocketDir := filepath.Join(tmp, "striatum")
		if err := os.MkdirAll(userSocketDir, 0o700); err != nil {
			t.Fatal(err)
		}
		userSock := filepath.Join(userSocketDir, "daemon-go.sock")
		if err := os.WriteFile(userSock, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := defaultSocketPath(map[string]string{
			"XDG_RUNTIME_DIR": tmp,
		})
		if got != userSock {
			t.Fatalf("socket = %q, want user socket %q", got, userSock)
		}
	})

	t.Run("SystemSocketFallbackWhenUserSocketAbsent", func(t *testing.T) {
		// Core #541 regression: when no STRIATUM_DAEMON_RUNTIME_DIR and the
		// user-scope socket does not exist, defaultSocketPath should return
		// SystemDaemonSocket when it exists on disk. This test only runs when
		// the system daemon socket is present (i.e. striatumd is running as a
		// system unit), which is the exact topology the fix targets.
		if _, err := os.Stat(SystemDaemonSocket); err != nil {
			t.Skipf("system socket %s not present on this host; skipping system-fallback subtest", SystemDaemonSocket)
		}
		tmp := t.TempDir() // XDG dir with NO user daemon socket inside
		got := defaultSocketPath(map[string]string{
			"XDG_RUNTIME_DIR": tmp,
		})
		if got != SystemDaemonSocket {
			t.Fatalf("socket = %q, want system fallback %q when user socket absent and system socket present", got, SystemDaemonSocket)
		}
	})

	t.Run("DefaultsToUserSocketWhenNeitherExists", func(t *testing.T) {
		// When no env is set and no socket exists on disk (neither user nor system),
		// return the user-scope path so the caller gets a legible daemon_unreachable
		// error naming the path tried. We simulate "system socket absent" by using
		// a nonexistent path — we can't override SystemDaemonSocket (const), so we
		// verify the behaviour when XDG_RUNTIME_DIR points somewhere isolated and
		// the system socket does NOT exist. On hosts where the system socket does
		// exist we skip this variant (it would be superseded by the system fallback).
		if _, err := os.Stat(SystemDaemonSocket); err == nil {
			t.Skipf("system socket %s exists; tier-4 default path unreachable on this host", SystemDaemonSocket)
		}
		tmp := t.TempDir()
		got := defaultSocketPath(map[string]string{
			"XDG_RUNTIME_DIR": tmp,
		})
		wantUserSock := filepath.Join(tmp, "striatum", "daemon-go.sock")
		if got != wantUserSock {
			t.Fatalf("socket = %q, want user-default %q when neither socket exists", got, wantUserSock)
		}
	})

	t.Run("RuntimeDirEnvFallsBackToBareSocket", func(t *testing.T) {
		// When STRIATUM_DAEMON_RUNTIME_DIR is set but rpc/daemon-go.sock does not
		// exist, fall back to <runtimeDir>/daemon-go.sock (non-nested layout).
		tmp := t.TempDir()
		got := defaultSocketPath(map[string]string{
			"STRIATUM_DAEMON_RUNTIME_DIR": tmp,
		})
		want := filepath.Join(tmp, "daemon-go.sock")
		if got != want {
			t.Fatalf("socket = %q, want %q", got, want)
		}
	})
}

func TestResolveConfigUsesExplicitAndEnvValues(t *testing.T) {
	config, err := ResolveConfig([]string{
		"STRIATUM_DAEMON_SOCKET=/env/socket",
		"STRIATUM_DAEMON_TOKEN=env-token",
	}, "/explicit/socket", "explicit-token", "", 123)
	if err != nil {
		t.Fatal(err)
	}
	if config.SocketPath != "/explicit/socket" || config.Token != "explicit-token" || config.DeadlineMS != 123 {
		t.Fatalf("config = %#v", config)
	}
}

func TestResolveConfigUsesRuntimeTokenFile(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("STRIATUM_DAEMON_RUNTIME_DIR", runtimeDir)
	config, err := ResolveConfig([]string{"XDG_RUNTIME_DIR=/tmp/runtime"}, "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if config.TokenFile != filepath.Join(runtimeDir, "client-token") {
		t.Fatalf("token file = %q", config.TokenFile)
	}
	if config.DeadlineMS != DefaultDeadlineMS {
		t.Fatalf("deadline = %d", config.DeadlineMS)
	}
}

func TestClientMapsDaemonRPCError(t *testing.T) {
	err := rpcError(rpc.Response{OK: false, Data: map[string]any{"code": "repo_not_registered", "message": "missing repo"}})
	if ExitCode(err) != 12 {
		t.Fatalf("exit code = %d", ExitCode(err))
	}
}

// TestRPCErrorThreadsSuggestion confirms rpcError reads the RFC 0111 suggestion
// off the daemon response into the client Error so writeError can surface it (#358).
func TestRPCErrorThreadsSuggestion(t *testing.T) {
	err := rpcError(rpc.Response{OK: false, Data: map[string]any{
		"code":       "blob_apply_required",
		"message":    "bucket does not exist",
		"suggestion": "Re-run repo init with apply enabled.",
	}})
	var clientErr *Error
	if !errors.As(err, &clientErr) {
		t.Fatalf("error is not *rpcclient.Error: %T", err)
	}
	if clientErr.Suggestion != "Re-run repo init with apply enabled." {
		t.Fatalf("suggestion = %q, want the daemon-supplied remediation", clientErr.Suggestion)
	}
}

func TestClientInvokeReadsTokenAndUsesEnvelope(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := rpc.NewServer()
	server.Register("status", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.CapabilityToken != "tok.secret" {
			t.Fatalf("token = %q", envelope.CapabilityToken)
		}
		if envelope.Params["repository_id"] != "repo_1" {
			t.Fatalf("params = %#v", envelope.Params)
		}
		return map[string]any{"state": "ok"}, nil
	})
	listener, err := rpc.ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx, listener)
	}()

	data, err := Client{Config: Config{SocketPath: socket, TokenFile: tokenFile, DeadlineMS: 1000}}.Invoke(context.Background(), "status", map[string]any{"repository_id": "repo_1"})
	if err != nil {
		t.Fatal(err)
	}
	if data["state"] != "ok" {
		t.Fatalf("data = %#v", data)
	}
}

func TestClientInvokeFallsBackToDiscoveryTokenAndRepairsRuntimeToken(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	runtimeDir := t.TempDir()
	tokenFile := filepath.Join(runtimeDir, "client-token")
	if err := os.WriteFile(tokenFile, []byte("stale.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "discovery.json"), []byte(`{"client_token":"valid.secret"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	authorizer := &recordingTokenAuthorizer{valid: "valid.secret"}
	server := rpc.NewServer()
	server.Authorizer = authorizer
	server.Register("status", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.CapabilityToken != "valid.secret" {
			t.Fatalf("handler saw token %q, want discovery fallback token", envelope.CapabilityToken)
		}
		return map[string]any{"state": "ok"}, nil
	})
	listener, err := rpc.ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = server.Serve(ctx, listener)
	}()

	data, err := Client{Config: Config{SocketPath: socket, TokenFile: tokenFile, DeadlineMS: 1000}}.Invoke(context.Background(), "status", map[string]any{"repository_id": "repo_1"})
	if err != nil {
		t.Fatal(err)
	}
	if data["state"] != "ok" {
		t.Fatalf("data = %#v", data)
	}
	if got := authorizer.tokens(); len(got) != 2 || got[0] != "stale.secret" || got[1] != "valid.secret" {
		t.Fatalf("auth tokens = %v, want stale token then discovery token", got)
	}
	body, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "valid.secret\n" {
		t.Fatalf("runtime token file = %q, want repaired discovery token", string(body))
	}
}

// TestClientInvokeDeadlineWhenDaemonWedgesAfterAccept is the #358 item-3
// regression: a daemon that accepts the connection then wedges before responding
// must NOT hang the client read forever. The CLI invokes with
// context.Background() (no context deadline), so the only thing that bounds the
// read is the envelope deadline_ms applied client-side in send(). With a short
// DeadlineMS the Invoke must return a daemon_unreachable error well before the
// test's own watchdog fires.
func TestClientInvokeDeadlineWhenDaemonWedgesAfterAccept(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "daemon.sock")
	tokenFile := filepath.Join(t.TempDir(), "client-token")
	if err := os.WriteFile(tokenFile, []byte("tok.secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A bare listener that accepts the connection and then wedges: it holds the
	// conn open and never writes a response line, exactly like a daemon stuck
	// after accept. DialTimeout succeeds (connect completes); reader.ReadBytes
	// would block indefinitely without a client-side deadline.
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		accepted <- conn // hold it open; never respond
	}()

	done := make(chan error, 1)
	go func() {
		_, invokeErr := Client{Config: Config{SocketPath: socket, TokenFile: tokenFile, DeadlineMS: 200}}.
			Invoke(context.Background(), "status", map[string]any{"repository_id": "repo_1"})
		done <- invokeErr
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Invoke against a wedged-after-accept daemon returned nil error; expected a deadline-driven failure")
		}
		var clientErr *Error
		if !errors.As(err, &clientErr) || clientErr.Code != "daemon_unreachable" {
			t.Fatalf("Invoke error = %v, want a daemon_unreachable client Error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Invoke hung past 5s against a wedged-after-accept daemon; client read deadline is not applied")
	}

	if conn := drainConn(accepted); conn != nil {
		_ = conn.Close()
	}
}

func drainConn(ch <-chan net.Conn) net.Conn {
	select {
	case c := <-ch:
		return c
	default:
		return nil
	}
}

type recordingTokenAuthorizer struct {
	mu    sync.Mutex
	valid string
	seen  []string
}

func (a *recordingTokenAuthorizer) Authorize(required *rpc.Capability, repositoryID string, token string) rpc.AuthContext {
	a.mu.Lock()
	a.seen = append(a.seen, token)
	a.mu.Unlock()
	if token == a.valid {
		return rpc.AuthContext{RepositoryID: repositoryID, Capability: *required, Decision: "allowed"}
	}
	return rpc.AuthContext{RepositoryID: repositoryID, Decision: "denied", DenialReason: "token_invalid"}
}

func (a *recordingTokenAuthorizer) tokens() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}
