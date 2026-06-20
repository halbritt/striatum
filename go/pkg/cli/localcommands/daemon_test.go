package localcommands

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/admin"
	"github.com/halbritt/striatum/go/pkg/cli/rpcclient"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

func TestDaemonSubcommandsAreLocal(t *testing.T) {
	for _, sub := range []string{"install", "uninstall", "status"} {
		if _, ok := Lookup([]string{"daemon", sub}); !ok {
			t.Fatalf("daemon %s not registered as a local command", sub)
		}
	}
}

func TestRenderUnitUsesSpecifiersNotHardcodedHome(t *testing.T) {
	unit := renderUnit()
	if !strings.Contains(unit, "%h/.local/bin/striatumd") {
		t.Fatalf("unit missing %%h-relative ExecStart:\n%s", unit)
	}
	if !strings.Contains(unit, "%t/striatum/daemon-go.sock") {
		t.Fatalf("unit missing %%t runtime socket:\n%s", unit)
	}
	if strings.Contains(unit, "striatumd.sock") {
		t.Fatalf("unit references retired striatumd.sock:\n%s", unit)
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(unit, home) {
		t.Fatalf("unit contains hardcoded home path %q:\n%s", home, unit)
	}
	// RFC 0103 W3 (#141): a daemon restart must signal only the main process so
	// the Setsid-detached supervisor helpers and tmux-backed agent lanes survive.
	if !strings.Contains(unit, "KillMode=process") {
		t.Fatalf("unit missing KillMode=process (#141: restart must not cgroup-kill lane helpers):\n%s", unit)
	}
	// A deterministic config error exits 78 (EX_CONFIG); the unit must tell
	// systemd not to auto-restart that exit, or a config typo crash-loops under
	// Restart=on-failure.
	if !strings.Contains(unit, "RestartPreventExitStatus=78") {
		t.Fatalf("unit missing RestartPreventExitStatus=78 (config errors must not crash-loop):\n%s", unit)
	}
}

func TestRenderUnitRepairsRuntimeDirectoryMode(t *testing.T) {
	unit := renderUnit()
	if !strings.Contains(unit, "ExecStartPre=/usr/bin/mkdir -p %t/striatum") {
		t.Fatalf("unit missing runtime directory creation:\n%s", unit)
	}
	if !strings.Contains(unit, "ExecStartPre=/usr/bin/chmod 0700 %t/striatum") {
		t.Fatalf("unit missing runtime directory permission repair:\n%s", unit)
	}
}

func TestDaemonInstallPrintUnit(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := RunDaemon([]string{"install", "--print-unit"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("print-unit exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ExecStart=%h/.local/bin/striatumd") {
		t.Fatalf("print-unit output missing ExecStart:\n%s", stdout.String())
	}
}

func TestScaffoldDaemonTOMLNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.toml")

	created, err := scaffoldDaemonTOML(path)
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if !created {
		t.Fatal("expected scaffold to create the file")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "postgres_url") {
		t.Fatalf("scaffold missing postgres_url guidance:\n%s", body)
	}

	// Simulate a real config and confirm a second call leaves it untouched.
	real := "postgres_url = \"postgres://real@localhost/striatum\"\n"
	if err := os.WriteFile(path, []byte(real), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = scaffoldDaemonTOML(path)
	if err != nil {
		t.Fatalf("scaffold (existing): %v", err)
	}
	if created {
		t.Fatal("scaffold overwrote an existing config")
	}
	body, _ = os.ReadFile(path)
	if string(body) != real {
		t.Fatalf("scaffold mutated existing config:\n%s", body)
	}
}

func TestResolveLayoutUsesCanonicalSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	configHomeDir := t.TempDir()
	t.Setenv("STRIATUM_DAEMON_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", configHomeDir)
	t.Setenv(rpcclient.EnvDaemonSocket, "")

	l, err := resolveLayout()
	if err != nil {
		t.Fatalf("resolveLayout: %v", err)
	}
	if !strings.HasSuffix(l.socket, "daemon-go.sock") {
		t.Fatalf("socket not canonical: %s", l.socket)
	}
	if strings.Contains(l.socket, "striatumd.sock") {
		t.Fatalf("socket references retired name: %s", l.socket)
	}
	wantUnit := filepath.Join(configHomeDir, "systemd", "user", "striatumd.service")
	if l.unitPath != wantUnit {
		t.Fatalf("unit path = %s, want %s", l.unitPath, wantUnit)
	}
}

func TestResolveLayoutUsesConfiguredDaemonSocket(t *testing.T) {
	runtimeDir := t.TempDir()
	configHomeDir := t.TempDir()
	socket := filepath.Join(runtimeDir, "rpc", "daemon-go.sock")
	t.Setenv("STRIATUM_DAEMON_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", configHomeDir)
	t.Setenv(rpcclient.EnvDaemonSocket, socket)

	l, err := resolveLayout()
	if err != nil {
		t.Fatalf("resolveLayout: %v", err)
	}
	if l.socket != socket {
		t.Fatalf("socket = %s, want configured %s", l.socket, socket)
	}
}

func TestInspectDaemonUnitPrefersActiveSystemUnit(t *testing.T) {
	restore := stubSystemctl(t, map[string]string{
		"--user show striatumd.service --property=FragmentPath --value": "",
		"--user is-enabled striatumd.service":                           "not-found",
		"--user is-active striatumd.service":                            "inactive",
		"show striatumd.service --property=FragmentPath --value":        "/etc/systemd/system/striatumd.service",
		"is-enabled striatumd.service":                                  "enabled",
		"is-active striatumd.service":                                   "active",
	})
	defer restore()

	unit := inspectDaemonUnit(layout{unitPath: filepath.Join(t.TempDir(), "striatumd.service")})
	if unit.Scope != systemdScopeSystem {
		t.Fatalf("unit scope = %s, want system", unit.Scope)
	}
	if unit.Path != "/etc/systemd/system/striatumd.service" || !unit.Installed || unit.Active != "active" {
		t.Fatalf("unit = %#v", unit)
	}
}

func TestRunDaemonStatusUsesDiscoveryTokenWhenClientTokenMissing(t *testing.T) {
	runtimeDir := t.TempDir()
	configHomeDir := t.TempDir()
	socket := filepath.Join(runtimeDir, "rpc", "daemon-go.sock")
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		t.Fatal(err)
	}
	token := "dtok_status.valid"
	t.Setenv(admin.EnvRuntimeDir, runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", configHomeDir)
	t.Setenv("STRIATUM_DAEMON_SOCKET", socket)
	if err := os.WriteFile(filepath.Join(runtimeDir, "discovery.json"), []byte(`{"client_token":"`+token+`"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	authorizer := rpc.NewMemoryAuthorizer()
	authorizer.AddToken(token, "client_status", map[rpc.Capability]rpc.CapabilityGrant{
		rpc.CapabilityRead: {},
	}, time.Now().Add(time.Hour))
	server := rpc.NewServer()
	server.Authorizer = authorizer
	server.Register("repo.resolve", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.CapabilityToken != token {
			t.Fatalf("repo.resolve token = %q, want discovery token", envelope.CapabilityToken)
		}
		return map[string]any{"repository_id": "repo_status", "repo_root": "."}, nil
	})
	server.Register("doctor", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if envelope.CapabilityToken != token {
			t.Fatalf("doctor token = %q, want discovery token", envelope.CapabilityToken)
		}
		return map[string]any{"ok": true}, nil
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

	var stdout, stderr bytes.Buffer
	if code := RunDaemon([]string{"status", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("status exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), token) {
		t.Fatalf("status leaked token material:\n%s", stdout.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, stdout.String())
	}
	data, _ := payload["data"].(map[string]any)
	if data["doctor"] != "ok" {
		t.Fatalf("doctor = %#v, want ok; payload=%s", data["doctor"], stdout.String())
	}
	if data["socket"] != socket || data["socket_present"] != true {
		t.Fatalf("socket status = (%#v, %#v), want (%q, true); payload=%s", data["socket"], data["socket_present"], socket, stdout.String())
	}
	if data["token_source"] != "discovery.json" {
		t.Fatalf("token_source = %#v, want discovery.json", data["token_source"])
	}
}

func stubSystemctl(t *testing.T, outputs map[string]string) func() {
	t.Helper()
	origLookPath := systemctlLookPath
	origOutput := systemctlOutputFn
	systemctlLookPath = func(file string) (string, error) {
		return "/bin/" + file, nil
	}
	systemctlOutputFn = func(args ...string) string {
		return outputs[strings.Join(args, " ")]
	}
	return func() {
		systemctlLookPath = origLookPath
		systemctlOutputFn = origOutput
	}
}

func TestAuthorizationFailureExplanationClassifiesTokenProblems(t *testing.T) {
	tests := []struct {
		name       string
		token      tokenInspection
		doctor     doctorStatus
		want       string
		wantReason string
	}{
		{
			name:       "missing token",
			token:      tokenInspection{Problem: "missing token"},
			doctor:     doctorStatus{ErrorCode: "token_unavailable"},
			want:       "missing token",
			wantReason: "missing_token",
		},
		{
			name:       "unreadable token",
			token:      tokenInspection{Problem: "read daemon capability token: permission denied"},
			doctor:     doctorStatus{ErrorCode: "token_unavailable"},
			want:       "unreadable token",
			wantReason: "unreadable_token",
		},
		{
			name:       "stale token",
			token:      tokenInspection{Source: "/run/user/1000/striatum/client-token", Present: true},
			doctor:     doctorStatus{ErrorCode: "token_invalid"},
			want:       "stale or revoked token",
			wantReason: "stale_or_revoked_token",
		},
		{
			name:       "daemon denial",
			token:      tokenInspection{Source: "/run/user/1000/striatum/client-token", Present: true},
			doctor:     doctorStatus{ErrorCode: "capability_missing"},
			want:       "daemon-side denial",
			wantReason: "daemon_denial",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			explanation := explainAuthorizationFailure(tt.doctor, tt.token)
			if explanation.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", explanation.Reason, tt.wantReason)
			}
			if !strings.Contains(explanation.Message, tt.want) {
				t.Fatalf("message = %q, want substring %q", explanation.Message, tt.want)
			}
		})
	}
}
