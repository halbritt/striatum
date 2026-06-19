package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/halbritt/striatum/go/pkg/admin"
	"github.com/halbritt/striatum/go/pkg/agentloop"
	daemonapply "github.com/halbritt/striatum/go/pkg/apply"
	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/crossrepo"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/mcp"
	"github.com/halbritt/striatum/go/pkg/mutations"
	"github.com/halbritt/striatum/go/pkg/reads"
	recoverypkg "github.com/halbritt/striatum/go/pkg/recovery"
	"github.com/halbritt/striatum/go/pkg/repositories"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
)

var (
	daemonVersion    = "go-dev"
	buildGitSHA      = "unknown"
	buildDirty       = "unknown"
	globalSocketPath string
)

func main() {
	var socketPath string
	var postgresURL string
	var migrate bool
	var describe bool
	var migrationsSHASource string
	var sweepIntervalSeconds float64
	var maxSweeps optionalIntFlag
	var autoSpawnScheduler bool
	var autoSpawnIntervalSeconds float64
	var agentLoop bool
	var checkConfig bool
	var mcpHTTPAddr string
	var webTailscale bool
	var auditHashFormat string
	var pgWriteBoundary string
	var recallDigest bool
	var recallDigestLimit int
	var recallDigestTimeoutMS int
	flag.StringVar(&socketPath, "socket", defaultSocketPath(), "Unix socket path")
	flag.StringVar(&auditHashFormat, "audit-hash-format", envOr("STRIATUM_AUDIT_HASH_FORMAT", "v2"), "RFC 0110 §5.2: audit row hash format (v2|v3); default v2. Flipping to v3 is the forward-only cutover and requires the owner bundle applied + a restart.")
	flag.StringVar(&pgWriteBoundary, "pg-write-boundary", envOr("STRIATUM_PG_WRITE_BOUNDARY", ""), "RFC 0110 §7: phased write-closure phase (none|audit_only|audit_artifacts|full). Empty derives from --audit-hash-format (v3 => audit_only). Each phase routes its surfaces through the owner-owned SECURITY DEFINER append functions and requires the matching owner bundle applied + a restart, set in lockstep.")
	flag.BoolVar(&webTailscale, "web-tailscale", envBool("STRIATUM_DAEMON_WEB_TAILSCALE"), "RFC 0085: serve a read-only tailnet-identity UI on a dedicated 0600 unix socket ($STRIATUM_DAEMON_RUNTIME_DIR/web-ui.sock) for `tailscale serve`; default off; loopback bind + bearer path unchanged")
	flag.BoolVar(&recallDigest, "recall-digest", envBool("STRIATUM_RECALL_DIGEST"), "RFC 0119: render a default-off .striatum/memory/relevant.md shelf after worktree.create commits")
	flag.IntVar(&recallDigestLimit, "recall-digest-limit", envInt("STRIATUM_RECALL_DIGEST_LIMIT", reads.RecallDefaultLimit), "RFC 0119: maximum recall shelf hits; capped at 20")
	flag.IntVar(&recallDigestTimeoutMS, "recall-digest-timeout-ms", envInt("STRIATUM_RECALL_DIGEST_TIMEOUT_MS", 1500), "RFC 0119: recall shelf read timeout in milliseconds")
	flag.StringVar(&postgresURL, "postgres-url", "", "PostgreSQL connection URL")
	flag.StringVar(&mcpHTTPAddr, "mcp-http-addr", defaultMCPHTTPAddr(), "loopback HTTP/SSE MCP listen address; use 'off' to disable")
	flag.BoolVar(&migrate, "migrate", true, "apply daemon PostgreSQL migrations before serving when a URL is configured")
	flag.BoolVar(&describe, "describe", false, "print daemon metadata and exit")
	flag.BoolVar(&agentLoop, "agent-loop", false, "run as the interactive MCP agent loop instead of a daemon server")
	flag.BoolVar(&checkConfig, "check-config", false, "validate daemon configuration (Postgres URL, write-boundary, blob storage) with no side effects and exit; exit 78 on a config error")
	flag.StringVar(&migrationsSHASource, "migrations-sha-source", "", "verify embedded migration SHAs against SQL files at this path before serving")
	flag.Float64Var(&sweepIntervalSeconds, "sweep-interval-seconds", 60.0, "seconds between resident recovery sweeps")
	flag.Var(&maxSweeps, "max-sweeps", "maximum resident recovery sweeps before exiting; when set to 0, one startup sweep still runs")
	flag.BoolVar(&autoSpawnScheduler, "auto-spawn-scheduler", envBool("STRIATUM_AUTO_SPAWN_SCHEDULER"), "RFC 0122 (#212): run the daemon-side supervision.auto_spawn scheduler that spawns a run's lanes from its captured run-owner grant with no operator RPC. Default OFF — the daemon-initiated-spawn product boundary is opt-in per deployment, in addition to the per-lane auto_spawn opt-in.")
	flag.Float64Var(&autoSpawnIntervalSeconds, "auto-spawn-interval-seconds", 5.0, "seconds between auto_spawn scheduler sweeps (only when --auto-spawn-scheduler is set)")
	flag.Parse()
	if socketPath != "" {
		_ = os.Setenv(agentloop.EnvDaemonSocket, socketPath)
	}

	if describe {
		migrationSHAs, err := db.MigrationSHASet()
		if err != nil {
			log.Fatalf("load migration sha set: %v", err)
		}
		fmt.Printf(
			"core=go envelope=%d framing=%s supported_schema=%d methods_etag=%s daemon_version=%s git_sha=%s build_dirty=%s migration_count=%d\n",
			rpc.SupportedEnvelopeVersion,
			rpc.DefaultFraming,
			db.LatestDaemonDBVersion,
			rpc.MethodsETag(),
			daemonVersion,
			buildGitSHA,
			buildDirty,
			len(migrationSHAs),
		)
		return
	}

	if agentLoop {
		repoRoot := os.Getenv("STRIATUM_REPO")
		runID := os.Getenv("STRIATUM_RUN_ID")
		sessionID := os.Getenv("STRIATUM_SESSION_ID")

		if err := agentloop.Run(socketPath, repoRoot, runID, sessionID, flag.Args()); err != nil {
			log.Fatalf("agent-loop failed: %v", err)
		}
		os.Exit(0)
	}

	if checkConfig {
		os.Exit(runConfigCheck(postgresURL, pgWriteBoundary, auditHashFormat))
	}

	// Validate configuration before any side effect (runtime reservation, DB
	// connect, socket bind). A malformed config is deterministic: restarting
	// cannot fix it, and the unit's Restart=on-failure would otherwise crash-loop
	// a config typo. Exit 78 (EX_CONFIG); the unit's RestartPreventExitStatus=78
	// keeps the daemon parked in `failed` with the error rather than looping. This
	// runs the same validator as `-check-config`.
	if problems := daemonConfigProblems(postgresURL, pgWriteBoundary, auditHashFormat); len(problems) > 0 {
		for _, p := range problems {
			log.Printf("config error: %v", p)
		}
		log.Printf("striatumd refusing to start: %d configuration problem(s); fix the config and restart (systemd will not auto-restart a config error — exit %d)", len(problems), exitConfigError)
		os.Exit(exitConfigError)
	}

	if migrationsSHASource != "" {
		if err := db.VerifyMigrationsSHASource(migrationsSHASource); err != nil {
			log.Fatalf("migrations sha source check failed: %v", err)
		}
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	ctx, cancel := context.WithCancel(signalCtx)
	defer cancel()

	globalSocketPath = socketPath
	releaseDaemonRuntime, err := reserveDaemonRuntime(socketPath, os.Getpid())
	if err != nil {
		log.Fatalf("reserve daemon runtime: %v", err)
	}
	defer releaseDaemonRuntime()
	fatalf := func(format string, args ...any) {
		releaseDaemonRuntime()
		log.Fatalf(format, args...)
	}

	substrateSchema := 0
	var recorder *db.AuditRecorder
	var runner db.Runner
	var authorizer rpc.Authorizer
	var webServiceToken string
	config := db.ResolveConfig(postgresURL)
	// RFC 0039 V1.6 F2 (dogfood-047 codex finding): the daemon must
	// refuse to bind a socket without a configured Postgres URL.
	// Production has no use for an unauthenticated, no-audit daemon;
	// installing AllowAllAuthorizer{} as a default was a security
	// regression that let `daemon describe` run without recording an
	// audit row. Refuse here; AllowAllAuthorizer{} remains available
	// for unit tests that explicitly construct a server.
	if config.URL == "" {
		fatalf("striatumd refuses to start without a Postgres URL; pass --postgres-url or set STRIATUM_DAEMON_DB_URL")
	}
	{
		// RFC 0110 §9.1 (restart-safe L0 boot order): run the authority bootstrap
		// over the owner connection FIRST — in a two-role deployment this rotates
		// the runtime role's password to a fresh RAM-only value — THEN bring up
		// the runtime pool with the (possibly rotated) credential. Connecting the
		// runtime pool first would only survive the first boot: rotation
		// invalidates the password captured in daemon.toml, so a later restart
		// would present a stale password and crash-loop (Restart=on-failure).
		// Inert when no owner bundle is applied (authority schema absent): no
		// rotation, runtime pool connects with config.URL unchanged. Fail-closed
		// on any owner-connection failure (§9.2).
		booted, err := db.BootstrapAndConnect(ctx, db.BootstrapConfig{
			RuntimeURL:    config.URL,
			OwnerURL:      strings.TrimSpace(os.Getenv("STRIATUM_OWNER_DB_URL")),
			RuntimeRole:   runtimeRoleFromURL(config.URL),
			InstanceID:    daemonInstanceID(),
			DaemonVersion: daemonVersion,
		}, migrate)
		if err != nil {
			fatalf("daemon db connect/bootstrap failed: %v", err)
		}
		pool := booted.Pool
		substrateSchema = booted.SchemaVersion
		authResult := booted.Authority
		// pool is already connected with the rotated credential (L0 §9.1); no
		// reconnect dance is needed. Release it on shutdown.
		defer func() { pool.Close() }()
		runner = pool.Runner
		// RFC 0110 §7: resolve and install the write-boundary phase BEFORE the
		// capability-parity check, which derives its required capabilities from
		// the active phase (a P1/P2 binary that lacks the matching owner-bundle
		// stamp must fail closed naming it).
		writeBoundary, err := db.ResolveWriteBoundary(pgWriteBoundary, auditHashFormat)
		if err != nil {
			fatalf("daemon write-boundary phase: %v", err)
		}
		db.SetActiveWriteBoundary(writeBoundary)
		// RFC 0110 §8.2 (C-DEPLOY-CAPABILITY-PARITY): verify the binary and the
		// live schema agree on authority-bearing capabilities before serving
		// mutations. Inert when no owner bundle has stamped a capability; once
		// the N+1 owner bundle is applied, a binary that does not support a
		// stamped capability fails closed here (old-binary / authority-schema).
		// This runs AFTER the L0 bootstrap above: a capability-skewed binary on
		// an active authority schema will have rotated the credential once before
		// failing closed here. That is harmless (the next boot re-rotates) and
		// spec-compliant (§8.2 requires parity before serving, not before
		// rotating); the only residue is registry/credential churn during a
		// misconfigured-deploy crash loop — see the per-instance-id follow-up.
		if err := db.VerifyCapabilityParity(ctx, runner,
			db.RequiredAuthorityCapabilities(), db.SupportedAuthorityCapabilities()); err != nil {
			fatalf("daemon capability parity check failed: %v", err)
		}
		db.SetAuthorityRuntime(authResult.Secret, auditHashFormat, authResult.Posture, authResult.RotatorCollision)
		log.Printf("daemon authority: posture=%s audit_hash_format=%s pg_write_boundary=%s registered=%t rotator_collision=%t",
			authResult.Posture, auditHashFormat, writeBoundary, authResult.Registered, authResult.RotatorCollision)
		tokenPath, err := admin.RuntimeTokenPath()
		if err != nil {
			fatalf("resolve daemon runtime token path: %v", err)
		}
		bootstrap, err := admin.BootstrapRuntimeTokenIfNeeded(ctx, runner, tokenPath)
		if err != nil {
			fatalf("bootstrap daemon admin token failed: %v", err)
		}
		if bootstrap != nil {
			log.Printf("bootstrapped daemon admin client %s and wrote runtime token %s", bootstrap["client_id"], tokenPath)
		}
		// The runtime client token (bootstrapped above or pre-existing) is the
		// same bearer the MCP listener and CLI present. The mounted web service
		// reuses it to gate HTTP access and author its downstream RPC calls.
		webServiceToken = readRuntimeTokenFile(tokenPath)
		if webServiceToken == "" {
			// RFC 0084 D1 build-review finding: the runtime token is the web
			// service's bearer, and the web handler authenticates open when its
			// ServiceToken is empty. If the token file is unreadable, fail
			// CLOSED — inject an unguessable deny token so mounted /v1 routes
			// reject every request (401) while MCP keeps its own auth — rather
			// than mounting the web surface without authentication.
			webServiceToken = randomDenyToken()
			log.Printf("warning: daemon runtime token %s unreadable; web service locked with a deny token (/v1 will 401 until the token is restored)", tokenPath)
		}
		recorder = &db.AuditRecorder{Runner: pool.Runner, DaemonVersion: daemonVersion}
		authorizer = &rpc.PostgresAuthorizer{Runner: pool.Runner, Clock: time.Now, AuthoritySecret: authResult.Secret}
	}

	server := rpc.NewServer()
	server.DaemonVersion = daemonVersion
	server.SubstrateSchema = substrateSchema
	server.SealedApplyFunc = daemonapply.FallbackSigningKeyStatus
	server.Authorizer = authorizer
	server.AuditRecorder = recorder
	var shutdownOnce sync.Once
	shutdownHook := func(context.Context) error {
		go func() {
			// Let the RPC response flush before closing the listener.
			time.Sleep(50 * time.Millisecond)
			shutdownOnce.Do(cancel)
		}()
		return nil
	}
	blobClient, err := loadBlobClient()
	if err != nil {
		fatalf("blob client setup: %v", err)
	}
	if blobClient != nil {
		log.Printf("blob storage configured")
	}
	registerHandlers(server, runner, handlerOptions{
		ShutdownHook:     shutdownHook,
		KeyRotateHook:    daemonapply.RotateFallbackSigningKey,
		BlobClient:       blobClient,
		DaemonSocketPath: socketPath,
		MCPBootEpoch:     daemonBootEpoch(),
		RecallDigest: mutations.RecallDigestOptions{
			Enabled: recallDigest,
			Limit:   recallDigestLimit,
			Timeout: time.Duration(recallDigestTimeoutMS) * time.Millisecond,
		},
	})

	listener, err := rpc.ListenUnix(socketPath)
	if err != nil {
		fatalf("listen on %s: %v", socketPath, err)
	}
	log.Printf("striatumd-go listening on %s", socketPath)
	stopMCPHTTP, err := startMCPHTTPServer(ctx, cancel, mcpHTTPAddr, server, authorizer, runner, resolveWebServiceOptions(webServiceToken))
	if err != nil {
		_ = listener.Close()
		fatalf("start MCP HTTP/SSE server: %v", err)
	}
	if stopMCPHTTP != nil {
		defer stopMCPHTTP()
	}
	var stopWebUI func()
	if webTailscale {
		stopWebUI, err = startWebUISocket(ctx, server, resolveWebUIOptions(webServiceToken))
		if err != nil {
			_ = listener.Close()
			if stopMCPHTTP != nil {
				stopMCPHTTP()
			}
			fatalf("start tailnet-identity UI socket: %v", err)
		}
		if stopWebUI != nil {
			defer stopWebUI()
		}
	}
	if err := grantDaemonSocketAccessToLaneUser(socketPath); err != nil {
		_ = listener.Close()
		if stopMCPHTTP != nil {
			stopMCPHTTP()
		}
		if stopWebUI != nil {
			stopWebUI()
		}
		fatalf("grant lane access to daemon socket: %v", err)
	}
	schedulerErr := startRecoveryScheduler(ctx, cancel, runner, sweepIntervalSeconds, maxSweeps)
	var autoSpawnErr <-chan error
	if autoSpawnScheduler {
		log.Printf("striatumd-go auto_spawn scheduler enabled (RFC 0122); sweeping every %.0fs", autoSpawnIntervalSeconds)
		autoSpawnErr = startAutoSpawnScheduler(ctx, cancel, runner, autoSpawnIntervalSeconds)
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	if err := server.Serve(ctx, listener); err != nil && ctx.Err() == nil {
		cancel()
		if stopMCPHTTP != nil {
			stopMCPHTTP()
		}
		fatalf("serve: %v", err)
	}
	cancel()
	if schedulerErr != nil {
		err := <-schedulerErr
		if err != nil && !errors.Is(err, context.Canceled) {
			if stopMCPHTTP != nil {
				stopMCPHTTP()
			}
			fatalf("recovery scheduler: %v", err)
		}
	}
	if autoSpawnErr != nil {
		err := <-autoSpawnErr
		if err != nil && !errors.Is(err, context.Canceled) {
			if stopMCPHTTP != nil {
				stopMCPHTTP()
			}
			fatalf("auto_spawn scheduler: %v", err)
		}
	}
}

func startMCPHTTPServer(ctx context.Context, cancel context.CancelFunc, addr string, rpcServer *rpc.Server, authorizer rpc.Authorizer, runner db.Runner, webOpts webServiceOptions) (func(), error) {
	value := strings.TrimSpace(addr)
	if value == "" {
		value = "127.0.0.1:0"
	}
	if strings.EqualFold(value, "off") || strings.EqualFold(value, "disabled") || strings.EqualFold(value, "none") {
		return nil, nil
	}
	listener, err := listenMCPHTTP(value)
	if err != nil {
		return nil, err
	}
	endpoint := mcpEndpointURL(listener.Addr())
	endpointPath, err := writeMCPEndpointFile(endpoint)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	discoveryPath, err := writeDaemonDiscoveryFile(endpoint, webOpts, listener)
	if err != nil {
		_ = listener.Close()
		_ = os.Remove(endpointPath)
		return nil, err
	}
	// #316: publish this process run's boot epoch alongside the endpoint so a
	// client can read the EXPECTED epoch of the daemon it dialed, and hold the
	// same value in the live MCP handler so every request can be checked against
	// it. The runtime file is best-effort: if it cannot be written the handler
	// still enforces (lanes carry the epoch via injected env/headers, not this
	// file), so a write failure must not abort daemon startup.
	bootEpoch := daemonBootEpoch()
	bootEpochPath, bootEpochErr := writeBootEpochFile(bootEpoch)
	if bootEpochErr != nil {
		log.Printf("striatumd-go MCP boot epoch file unavailable (continuing; enforcement still active): %v", bootEpochErr)
		bootEpochPath = ""
	}
	mcpHandler := mcp.NewHTTPHandler(mcp.Service{
		RPC:              rpcServer,
		Authorizer:       authorizer,
		ActivityRecorder: sessionliveness.DBRecorder{Runner: runner},
		BootEpoch:        bootEpoch,
	})
	webHandler := newWebServiceHandler(rpcServer, webOpts)
	httpServer := &http.Server{
		Handler:           newDaemonHTTPHandler(mcpHandler, webHandler),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("striatumd-go MCP HTTP/SSE listening on %s (web service mounted at /v1)", endpoint)
	log.Printf("striatumd-go MCP endpoint file %s", endpointPath)
	log.Printf("striatumd-go discovery file %s", discoveryPath)
	if bootEpochPath != "" {
		log.Printf("striatumd-go MCP boot epoch file %s", bootEpochPath)
	}
	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("MCP HTTP/SSE server stopped: %v", err)
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	return func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = httpServer.Shutdown(shutdownCtx)
		_ = listener.Close()
		if err := os.Remove(endpointPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove MCP endpoint file %s: %v", endpointPath, err)
		}
		if err := os.Remove(discoveryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove discovery file %s: %v", discoveryPath, err)
		}
		if bootEpochPath != "" {
			if err := os.Remove(bootEpochPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("remove MCP boot epoch file %s: %v", bootEpochPath, err)
			}
		}
	}, nil
}

func writeDaemonDiscoveryFile(endpoint string, webOpts webServiceOptions, listener net.Listener) (string, error) {
	runtimeDir, err := admin.RuntimeDir()
	if err != nil {
		return "", err
	}

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	var portInt int
	if err == nil {
		portInt, _ = strconv.Atoi(portStr)
	}

	data := map[string]any{
		"pid":           os.Getpid(),
		"socket_path":   globalSocketPath,
		"mcp_http_url":  endpoint,
		"mcp_http_port": portInt,
		"client_token":  webOpts.ServiceToken,
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	discoveryPath := filepath.Join(runtimeDir, "discovery.json")
	if err := writeOwnerOnlyJSONFile(discoveryPath, encoded); err != nil {
		return "", err
	}

	return discoveryPath, nil
}

func writeOwnerOnlyJSONFile(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmpFile, err := os.CreateTemp(dir, "discovery-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if err := tmpFile.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmpFile.Write(content); err != nil {
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func listenMCPHTTP(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid MCP HTTP listen address %q: %w", addr, err)
	}
	if host == "" {
		return nil, fmt.Errorf("MCP HTTP listen address %q must bind an explicit loopback host", addr)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("MCP HTTP listen address %q must bind loopback only", addr)
		}
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok && !tcpAddr.IP.IsLoopback() {
		_ = listener.Close()
		return nil, fmt.Errorf("MCP HTTP listen address %q resolved to non-loopback address %s", addr, tcpAddr.IP.String())
	}
	return listener, nil
}

func mcpEndpointURL(addr net.Addr) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String() + mcp.EndpointPath
	}
	return "http://" + net.JoinHostPort(host, port) + mcp.EndpointPath
}

func writeMCPEndpointFile(endpoint string) (string, error) {
	path, err := admin.RuntimeMCPEndpointPath()
	if err != nil {
		return "", err
	}
	if err := writeOwnerOnlyTextFile(path, endpoint+"\n"); err != nil {
		return "", fmt.Errorf("write MCP endpoint file %s: %w", path, err)
	}
	return path, nil
}

// readRuntimeTokenFile reads the daemon runtime client token, returning "" when
// the file is absent or unreadable. An empty token must not be passed to the
// mounted web service as-is (its authenticate() treats an empty ServiceToken as
// open); callers substitute randomDenyToken to fail closed.
func readRuntimeTokenFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// randomDenyToken returns an unguessable bearer the web service requires but
// nobody holds, so /v1 requests fail closed (401) when the runtime token is
// unreadable. On RNG failure it still returns a non-empty constant so the web
// service never authenticates open.
func randomDenyToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "deny-web-token-rng-unavailable"
	}
	return "deny-" + hex.EncodeToString(buf)
}

// runtimeRoleFromURL extracts the runtime role from the runtime DSN (the role
// whose password L0 rotates and whose authority secret is registered). Defaults
// to striatumd_rw when the URL carries no username.
func runtimeRoleFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User == nil || parsed.User.Username() == "" {
		return "striatumd_rw"
	}
	return parsed.User.Username()
}

var daemonInstanceIDOnce struct {
	once  sync.Once
	value string
}

// daemonInstanceID returns this daemon installation's instance id (RFC 0110 §9.1
// registry key). It is stable across restarts: it is persisted in the daemon
// runtime dir and read back on the next boot, so a restart UPSERTs the single
// existing daemon_auth_registry row instead of inserting a new one (GH #168). A
// fresh random id per process made the owner-owned registry grow one row per
// restart and tripped a false rotator_collision on any restart within the
// 5-minute role-scoped probe window (RFC 0110 §9.4).
func daemonInstanceID() string {
	daemonInstanceIDOnce.once.Do(func() {
		if dir, err := admin.RuntimeDir(); err == nil {
			if id, err := stableInstanceID(dir); err == nil {
				daemonInstanceIDOnce.value = id
				return
			}
		}
		// Fallback when the runtime dir is unavailable: an ephemeral random id
		// (pre-#168 behavior). Still correct; it just reverts to per-process
		// registry churn for this boot.
		daemonInstanceIDOnce.value = randomInstanceID()
	})
	return daemonInstanceIDOnce.value
}

// instanceIDFileName is the runtime-dir file that pins the daemon instance id
// across restarts.
const instanceIDFileName = "instance-id"

// bootEpochFileName is the runtime-dir sibling file that publishes THIS daemon
// process run's boot epoch so a client can read the epoch of the daemon it
// intends to talk to (#316, follow-up to #296).
const bootEpochFileName = "mcp-boot-epoch"

var daemonBootEpochOnce struct {
	once  sync.Once
	value string
}

// daemonBootEpoch returns this daemon PROCESS RUN's boot epoch (#316). Unlike
// daemonInstanceID (deliberately stable across restarts so the auth registry
// UPSERTs one row — GH #168), the boot epoch is fresh per process: it is
// generated once in memory at startup and NEVER persisted in a form that
// carries across a restart. That is exactly the property #316 needs — the MCP
// HTTP listener binds a dynamic port that the OS can reuse, so a stale lane (or
// a stale on-disk config.toml pin) can dial a port number now bound by a
// DIFFERENT live daemon process run. The bearer authenticates and the request
// would otherwise reach a real-but-wrong daemon, letting the lane touch another
// active run's workflow state. A per-process boot epoch lets the daemon reject
// such a request: a lane carries the epoch of the daemon it was launched
// against, the daemon compares it to its own live epoch, and a mismatch is a
// recycled-port hit even when the same installation merely restarted.
func daemonBootEpoch() string {
	daemonBootEpochOnce.once.Do(func() {
		daemonBootEpochOnce.value = randomBootEpoch()
	})
	return daemonBootEpochOnce.value
}

// randomBootEpoch mints a fresh per-process boot epoch, or "epoch-unknown" when
// the system CSPRNG is unavailable (still non-empty so the value is always
// presentable; an unknown epoch fails closed against any concrete presented
// epoch and matches only an equally-unknown one, which is acceptably rare).
func randomBootEpoch() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "epoch-unknown"
	}
	return "epoch-" + hex.EncodeToString(buf)
}

// writeBootEpochFile publishes the live boot epoch to the runtime-dir sibling
// file so a client can resolve the EXPECTED epoch of the daemon it intends to
// talk to. Owner-only (0600), atomically replaced; the same shape as the
// endpoint/instance-id runtime files.
func writeBootEpochFile(epoch string) (string, error) {
	runtimeDir, err := admin.RuntimeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(runtimeDir, bootEpochFileName)
	if err := writeOwnerOnlyTextFile(path, epoch+"\n"); err != nil {
		return "", fmt.Errorf("write MCP boot epoch file %s: %w", path, err)
	}
	return path, nil
}

// stableInstanceID returns the instance id persisted in runtimeDir, generating
// and persisting a fresh one (0600, owner-only) on first boot. A present but
// empty/whitespace file is treated as absent and replaced.
func stableInstanceID(runtimeDir string) (string, error) {
	path := filepath.Join(runtimeDir, instanceIDFileName)
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id, nil
		}
	}
	id := randomInstanceID()
	if id == "inst-unknown" {
		return "", errors.New("generate instance id: crypto/rand unavailable")
	}
	if err := writeOwnerOnlyTextFile(path, id+"\n"); err != nil {
		return "", err
	}
	return id, nil
}

// randomInstanceID mints a fresh random instance id, or "inst-unknown" when the
// system CSPRNG is unavailable.
func randomInstanceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "inst-unknown"
	}
	return "inst-" + hex.EncodeToString(buf)
}

func writeOwnerOnlyTextFile(path string, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func startRecoveryScheduler(ctx context.Context, cancel context.CancelFunc, runner db.Runner, sweepIntervalSeconds float64, maxSweepsFlag optionalIntFlag) <-chan error {
	done := make(chan error, 1)
	var maxSweeps *int
	if maxSweepsFlag.Provided {
		value := maxSweepsFlag.Value
		maxSweeps = &value
	}
	interval := time.Duration(sweepIntervalSeconds * float64(time.Second))
	go func() {
		// Per-run sweep panics are converted to degraded-cursor errors inside the
		// sweep loop (recovery/sweep.go runPerRunSweep), so one poison run no longer
		// reaches here. This goroutine-level recover is a backstop for a panic in the
		// outer scheduler machinery (top-level query, cursor upsert, scheduler loop):
		// it logs loud + stack and cancels the daemon for a clean, controlled
		// shutdown (systemd restart) instead of re-raising into an uncontrolled
		// process crash. It does NOT re-raise (issue #451 / FMA-001).
		defer func() {
			if r := recover(); r != nil {
				log.Printf("recovery scheduler goroutine panic recovered; cancelling daemon for clean restart: panic=%v\n%s", r, debug.Stack())
				cancel()
				done <- fmt.Errorf("recovery scheduler goroutine panic recovered: %v", r)
			}
		}()
		result, err := recoverypkg.RunScheduler(ctx, recoverypkg.SchedulerOptions{
			Interval:  interval,
			MaxSweeps: maxSweeps,
			SweepOnce: recoverypkg.ActiveRunSweep{
				Runner: runner,
				Author: "striatumd-go",
			}.SweepOnce,
			OnSweepError: func(err error) {
				log.Printf("recovery scheduler sweep failed; backing off until next interval: %v", err)
			},
		})
		if err == nil {
			log.Printf("recovery scheduler stopped after %d sweep(s): %s", result.Sweeps, result.Reason)
			if result.Reason == recoverypkg.ReasonMaxSweepsReached {
				cancel()
			}
		} else if !errors.Is(err, context.Canceled) {
			cancel()
		}
		done <- err
	}()
	return done
}

// startAutoSpawnScheduler runs the RFC 0122 supervision.auto_spawn scheduler on
// the resident scheduler loop (modeled on startRecoveryScheduler): each tick
// reconciles every running run that holds an active spawn-authorization grant,
// spawning its queued auto_spawn lanes under the captured run owner. A sweep
// error is logged and backed off (a poisoned spawn is also recorded as a degraded
// scheduler cursor); only an unrecoverable scheduler error cancels the daemon.
func startAutoSpawnScheduler(ctx context.Context, cancel context.CancelFunc, runner db.Runner, intervalSeconds float64) <-chan error {
	done := make(chan error, 1)
	interval := time.Duration(intervalSeconds * float64(time.Second))
	go func() {
		// Per-run spawn panics are converted to degraded-cursor errors inside the
		// sweep loop (recovery/sweep.go runPerRunSweep). This goroutine-level recover
		// is a backstop for a panic in the outer scheduler machinery: it logs loud +
		// stack and cancels the daemon for a clean, controlled restart instead of an
		// unhandled panic taking the single-writer process down (issue #451 /
		// FMA-001 — the auto_spawn loop previously had NO recover at all).
		defer func() {
			if r := recover(); r != nil {
				log.Printf("auto_spawn scheduler goroutine panic recovered; cancelling daemon for clean restart: panic=%v\n%s", r, debug.Stack())
				cancel()
				done <- fmt.Errorf("auto_spawn scheduler goroutine panic recovered: %v", r)
			}
		}()
		_, err := recoverypkg.RunScheduler(ctx, recoverypkg.SchedulerOptions{
			Interval: interval,
			SweepOnce: recoverypkg.AutoSpawnSweep{
				Runner: runner,
				Author: "striatumd-go",
			}.SweepOnce,
			OnSweepError: func(err error) {
				log.Printf("auto_spawn scheduler sweep failed; backing off until next interval: %v", err)
			},
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			cancel()
		}
		done <- err
	}()
	return done
}

type optionalIntFlag struct {
	Value    int
	Provided bool
}

func (f *optionalIntFlag) Set(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	f.Value = parsed
	f.Provided = true
	return nil
}

func (f *optionalIntFlag) String() string {
	if f == nil || !f.Provided {
		return ""
	}
	return strconv.Itoa(f.Value)
}

type handlerOptions struct {
	ShutdownHook     admin.ShutdownFunc
	KeyRotateHook    admin.KeyRotateFunc
	BlobClient       *blob.Client
	DaemonSocketPath string
	MCPBootEpoch     string
	RecallDigest     mutations.RecallDigestOptions
}

// loadBlobClient builds the daemon's S3 client from environment
// configuration. Returns (nil, nil) when blob storage is opt-out
// (STRIATUM_BLOB_ENDPOINT unset); returns a non-nil error for
// half-configured or malformed configuration. The daemon refuses to
// start when configuration is malformed but happily runs without blob
// storage when it is simply absent (RFC 0072 is opt-in for V1).
func loadBlobClient() (*blob.Client, error) {
	cfg, err := blob.LoadConfig()
	if blob.IsDisabled(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return blob.New(cfg)
}

func registerHandlers(server *rpc.Server, runner db.Runner, opts ...handlerOptions) {
	options := handlerOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	admin.Service{
		Runner:        runner,
		DaemonVersion: daemonVersion,
		ShutdownHook:  options.ShutdownHook,
		KeyRotateHook: options.KeyRotateHook,
		BlobClient:    options.BlobClient,
	}.Register(server)
	daemonapply.Service{Runner: runner}.Register(server)
	registerCrossRepoHandlers(server, runner)
	// RFC 0048 Phase B: register the Go-core read-surface handlers
	// before the not-implemented stub loop so the loop's existence-check
	// skips them. Mirrors src/striatum/daemon_pg/handlers/reads/ in
	// Python; same response shapes.
	reads.Register(server, runner, reads.Options{BlobClient: options.BlobClient, StriatumVersion: daemonVersion})
	mutations.Register(server, runner, mutations.Options{BlobClient: options.BlobClient, DaemonSocketPath: options.DaemonSocketPath, MCPBootEpoch: options.MCPBootEpoch, RecallDigest: options.RecallDigest})
	repositories.Service{Runner: runner, BlobClient: options.BlobClient}.Register(server)
	for _, method := range []string{
		"status", "why", "doctor", "dashboard", "dashboard.all",
		"evidence.export", "corpus.export", "run.summary", "run.graph",
		"workflow.validate", "workflow.plan", "workflow.graph",
		"workflow.templates.list", "workflow.templates.show",
		"workflow.generate.preview", "list.runs", "list.sessions",
		"list.jobs", "list.artifacts", "list.workflows", "worktree.list",
		"repo.list", "session.register", "session.close", "work.claim_next",
		"work.ack", "work.heartbeat", "work.release", "supervise.start",
		"supervise.send", "supervise.stop", "supervise.status",
		"supervise.list", "supervise.reattach_status", "work.send_message",
		"work.block", "work.complete", "artifact.publish", "worktree.create",
		"worktree.release", "workflow.init", "workflow.generate",
		"workflow.upgrade", "review.submit",
		"review.verdict", "review.override", "decision.record",
		"checkpoint.resolve", "branch.confirm", "run.prepare", "run.start",
		"run.pause", "run.resume", "run.cancel", "run.retry_job", "repo.init",
		"recovery.stale_leases", "recovery.requeue_stale",
		"recovery.cancel_job", "recovery.process_reconcile", "recovery.resume",
		"recovery.auto",
		"repo.add", "repo.remove", "daemon.token.create", "daemon.token.revoke",
		"daemon.token.rotate", "daemon.key.rotate", "daemon.shutdown",
		"daemon.migrate", "ack", "heartbeat",
		"release", "block", "complete", "publish_artifact", "claim_next",
		"verdict", "submit_review",
	} {
		if _, exists := server.Handlers[method]; exists {
			continue
		}
		server.Register(method, notImplementedHandler(method))
	}
}

func registerCrossRepoHandlers(server *rpc.Server, runner db.Runner) {
	local := localWorkflowRunner{runner: runner}
	server.Register("cross_repo.list", func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if runner == nil {
			return nil, rpc.NewError("daemon_db_missing", "cross-repo routes require daemon PostgreSQL", nil)
		}
		return crossrepo.ListRuns(ctx, runner)
	})
	server.Register("cross_repo.describe", func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if runner == nil {
			return nil, rpc.NewError("daemon_db_missing", "cross-repo routes require daemon PostgreSQL", nil)
		}
		runID := param(envelope.Params, "cross_repo_run_id")
		if runID == "" {
			runID = param(envelope.Params, "run_id")
		}
		if runID == "" {
			return nil, rpc.NewError("schema_invalid", "cross-repo route requires cross_repo_run_id", nil)
		}
		return crossrepo.DescribeRun(ctx, runner, runID)
	})
	server.Register("cross_repo.why", func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if runner == nil {
			return nil, rpc.NewError("daemon_db_missing", "cross-repo routes require daemon PostgreSQL", nil)
		}
		runID := param(envelope.Params, "cross_repo_run_id")
		if runID == "" {
			runID = param(envelope.Params, "run_id")
		}
		if runID == "" {
			return nil, rpc.NewError("schema_invalid", "cross-repo route requires cross_repo_run_id", nil)
		}
		return crossrepo.Why(ctx, runner, runID)
	})
	server.Register("cross_repo.cancel", func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		if runner == nil {
			return nil, rpc.NewError("daemon_db_missing", "cross-repo routes require daemon PostgreSQL", nil)
		}
		runID := param(envelope.Params, "cross_repo_run_id")
		if runID == "" {
			runID = param(envelope.Params, "run_id")
		}
		if runID == "" {
			return nil, rpc.NewError("schema_invalid", "cross-repo route requires cross_repo_run_id", nil)
		}
		reason := param(envelope.Params, "reason")
		if reason == "" {
			reason = "operator canceled cross-repo run"
		}
		return crossrepo.CancelRun(ctx, runner, runID, reason, local)
	})
}

type localWorkflowRunner struct {
	runner db.Runner
}

func (l localWorkflowRunner) Start(ctx context.Context, repositoryID string, localRunID string) error {
	_, err := mutations.HandleRunStart(ctx, l.runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "cross_repo_start_" + localRunID,
		Method:        "run.start",
		Params: map[string]any{
			"repository_id": repositoryID,
			"run_id":        localRunID,
		},
	})
	return err
}

func (l localWorkflowRunner) Cancel(ctx context.Context, repositoryID string, localRunID string, reason string) error {
	active, err := l.runner.QueryScalar(ctx, "SELECT repo_root FROM striatumd.repositories WHERE repository_id = $1 AND state = 'active'", repositoryID)
	if err != nil {
		return err
	}
	if active == "" {
		return fmt.Errorf("active repository not found: %s", repositoryID)
	}
	_, err = mutations.HandleRunCancel(ctx, l.runner, rpc.Envelope{
		SchemaVersion: rpc.SupportedEnvelopeVersion,
		RequestID:     "cross_repo_cancel_" + localRunID,
		Method:        "run.cancel",
		Params: map[string]any{
			"repository_id": repositoryID,
			"run_id":        localRunID,
			"reason":        reason,
		},
	})
	return err
}

func notImplementedHandler(method string) rpc.Handler {
	return func(ctx context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("not_implemented", fmt.Sprintf("%s is registered but not implemented in the Go daemon yet", method), nil)
	}
}

func param(params map[string]any, key string) string {
	if value, ok := params[key].(string); ok {
		return value
	}
	return ""
}

func defaultSocketPath() string {
	if socket := strings.TrimSpace(os.Getenv(agentloop.EnvDaemonSocket)); socket != "" {
		return socket
	}
	if runtimeDir := strings.TrimSpace(os.Getenv(admin.EnvRuntimeDir)); runtimeDir != "" {
		return filepath.Join(runtimeDir, "daemon-go.sock")
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	return filepath.Join(runtimeDir, "striatum", "daemon-go.sock")
}

func defaultMCPHTTPAddr() string {
	if value := os.Getenv("STRIATUM_DAEMON_MCP_HTTP_ADDR"); strings.TrimSpace(value) != "" {
		return value
	}
	return "127.0.0.1:0"
}
