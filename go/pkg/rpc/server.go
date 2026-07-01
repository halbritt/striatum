package rpc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime/debug"
)

type Handler func(context.Context, Envelope) (map[string]any, error)

type AuditRecorder interface {
	RecordRPC(context.Context, Envelope, AuthContext, Response) (string, error)
}

type TransportAuditRecorder interface {
	RecordRPCTransport(context.Context, Envelope, AuthContext, Response, string) (string, error)
}

type Server struct {
	DaemonVersion   string
	SubstrateSchema int
	SealedApply     map[string]any
	SealedApplyFunc func() map[string]any
	Authorizer      Authorizer
	AuditRecorder   AuditRecorder
	Handlers        map[string]Handler

	// seenRequests / handshakeSeen are bounded (size cap + TTL eviction) so a
	// pre-auth caller cannot grow daemon memory without bound: markRequest runs
	// in handle() BEFORE Authorizer.Authorize. boundedSeen is internally
	// synchronized, so the server needs no separate mutex for them.
	seenRequests  *boundedSeen
	handshakeSeen *boundedSeen
}

func NewServer() *Server {
	return &Server{
		DaemonVersion:   "unknown",
		SubstrateSchema: 0,
		SealedApply: map[string]any{
			"supported":  false,
			"key_loaded": false,
			"public_key": nil,
		},
		Authorizer:    AllowAllAuthorizer{},
		Handlers:      map[string]Handler{},
		seenRequests:  newBoundedSeen(defaultDedupeMaxEntries, defaultDedupeTTL),
		handshakeSeen: newBoundedSeen(defaultDedupeMaxEntries, defaultDedupeTTL),
	}
}

func (s *Server) Register(method string, handler Handler) {
	s.Handlers[method] = handler
}

func (s *Server) Handle(ctx context.Context, envelope Envelope, connectionID string) Response {
	return s.handle(ctx, envelope, connectionID, true)
}

func (s *Server) HandleWithoutHandshake(ctx context.Context, envelope Envelope, connectionID string) Response {
	return s.handle(ctx, envelope, connectionID, false)
}

func (s *Server) handle(ctx context.Context, envelope Envelope, connectionID string, requireHandshake bool) Response {
	transport := "rpc"
	if !requireHandshake && connectionID == "mcp" {
		transport = "mcp"
	}
	auth := AuthContext{RepositoryID: repositoryID(envelope.Params), Decision: "allowed"}
	// RFC 0110 §4.4: a mutating handler appends its audit row inside its own
	// transaction (atomic with the mutation) and records the result here, so the
	// dispatch layer can skip the standalone append for a row already written.
	// The dispatch (and thus in-transaction auditing) exists only when an audit
	// recorder is configured, so a recorder-less test server audits nothing.
	var auditDispatch *AuditDispatch
	if s.AuditRecorder != nil {
		auditDispatch = &AuditDispatch{DaemonVersion: s.DaemonVersion, Transport: transport}
	}
	if duplicate := s.markRequest(envelope.RequestID); duplicate {
		return ErrorResponse(envelope.RequestID, NewError("duplicate_request", "daemon RPC request_id was already used", nil), "")
	}

	var data map[string]any
	var err error
	entry, known := MethodRegistry[envelope.Method]
	if requireHandshake && envelope.Method != "daemon.hello" && !s.hasHandshake(connectionID) {
		err = NewError("version_incompatible", "daemon.hello must run before ordinary RPC routes", nil)
		auth = deniedAuth(auth, "version_incompatible")
	} else if !known {
		err = unknownMethodError(envelope.Method)
		auth = deniedAuth(auth, "method_unknown")
	} else if entry.RepositoryScope && repositoryID(envelope.Params) == "" {
		err = NewError("repo_not_registered", "daemon RPC route requires repository_id", nil)
		auth = deniedAuth(auth, "repo_not_registered")
	} else if envelope.Method == "daemon.hello" {
		data, err = s.buildWelcome(envelope.Params)
		if err == nil {
			s.markHandshake(connectionID)
		}
	} else {
		auth = s.Authorizer.Authorize(entry.RequiredCapability, repositoryID(envelope.Params), envelope.CapabilityToken)
		if auth.RepositoryID == "" {
			auth.RepositoryID = repositoryID(envelope.Params)
		}
		err = RequireAllowed(auth, envelope.Method, entry.RequiredCapability)
		if err == nil {
			// RFC 0096 V2 / #135: thread the resolved AuthContext onto the
			// context so session-scoped handlers can read the caller's bound
			// SessionID (if any) and enforce per-session binding without a
			// signature change. Threaded only after Authorize succeeds.
			// RFC 0110: also thread the envelope + audit dispatch so the
			// authority prelude can label the mutation transaction and a
			// mutating handler can couple its audit row to its own transaction.
			routeCtx := WithEnvelope(WithAuthContext(ctx, auth), envelope)
			if auditDispatch != nil {
				routeCtx = WithAuditDispatch(routeCtx, auditDispatch)
			}
			data, err = s.route(routeCtx, envelope)
		}
		if err != nil {
			if rpcErr := (&Error{}); errors.As(err, &rpcErr) {
				auth = deniedAuth(auth, rpcErr.Code)
			}
		}
	}

	var response Response
	if err != nil {
		response = ErrorResponse(envelope.RequestID, err, "")
	} else {
		response = OKResponse(envelope.RequestID, data, "")
	}
	// RFC 0110 §4.4: audit append is fail-closed. A mutating handler that wrote
	// its audit row inside its own transaction records it on the dispatch; honour
	// that and skip the standalone append. Otherwise (reads, denials, errors, and
	// mutations whose own transaction rolled back) append standalone and convert
	// an append failure into an error response — never a response without a row.
	if auditDispatch != nil && auditDispatch.Appended {
		response.AuditID = auditDispatch.AuditID
	} else if s.AuditRecorder != nil {
		var auditID string
		var auditErr error
		if recorder, ok := s.AuditRecorder.(TransportAuditRecorder); ok {
			auditID, auditErr = recorder.RecordRPCTransport(ctx, envelope, auth, response, transport)
		} else {
			auditID, auditErr = s.AuditRecorder.RecordRPC(ctx, envelope, auth, response)
		}
		if auditErr != nil {
			return ErrorResponse(envelope.RequestID, NewError(
				"audit_append_failed",
				"daemon could not append the RPC audit row; refusing to answer without provenance",
				map[string]any{"cause": auditErr.Error()},
			), "")
		}
		if auditID != "" {
			response.AuditID = auditID
		}
	}
	return response
}

var retiredMethodReplacements = map[string]string{
	"recovery.auto": "recovery.sweep",
}

func unknownMethodError(method string) *Error {
	if replacement, ok := retiredMethodReplacements[method]; ok {
		err := NewError("method_unknown", fmt.Sprintf("retired daemon RPC method: %s; use %s", method, replacement), map[string]any{
			"retired_method":     method,
			"replacement_method": replacement,
		})
		err.Suggestion = fmt.Sprintf("Use %s or the current CLI route documented in docs/reference/command-authority-matrix.md.", replacement)
		return err
	}
	return NewError("method_unknown", fmt.Sprintf("unknown daemon RPC method: %s", method), nil)
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	defer func() { _ = listener.Close() }()
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return err
			}
		}
		go func() {
			_ = s.ServeConn(ctx, conn, conn.RemoteAddr().String())
		}()
	}
}

// MaxEnvelopeBytes is the largest RPC envelope (newline-framed JSON
// line) the server accepts. bufio.Scanner defaults to 64 KiB which
// is too small for envelopes carrying base64-encoded artifact bodies
// (RFC 0072 corpus.migrate_historical_dogfood_file). 8 MiB gives the
// migration headroom while still bounding memory per connection.
const MaxEnvelopeBytes = 8 * 1024 * 1024

func (s *Server) ServeConn(ctx context.Context, rwc io.ReadWriteCloser, connectionID string) error {
	currentMethod := ""
	currentRequestID := ""
	defer func() {
		if r := recover(); r != nil {
			log.Printf("daemon RPC ServeConn panic: connection_id=%q method=%q request_id=%q panic=%v\n%s", connectionID, currentMethod, currentRequestID, r, debug.Stack())
			panic(r)
		}
	}()
	defer func() { _ = rwc.Close() }()
	reader := bufio.NewScanner(rwc)
	reader.Buffer(make([]byte, 64*1024), MaxEnvelopeBytes)
	writer := bufio.NewWriter(rwc)
	for reader.Scan() {
		line := reader.Bytes()
		if len(stripSpace(line)) == 0 {
			continue
		}
		currentMethod = ""
		currentRequestID = ""
		envelope, err := DecodeEnvelope(line)
		var response Response
		if err != nil {
			response = ErrorResponse("", err, "")
		} else {
			currentMethod = envelope.Method
			currentRequestID = envelope.RequestID
			response = s.Handle(ctx, envelope, connectionID)
		}
		encoded, err := response.Encode()
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(encoded, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return reader.Err()
}

func ListenUnix(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("socket path is required")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func (s *Server) route(ctx context.Context, envelope Envelope) (map[string]any, error) {
	if envelope.Method == "daemon.describe" {
		return DescribeMethods(), nil
	}
	handler, ok := s.Handlers[envelope.Method]
	if !ok {
		return nil, NewError("method_unknown", fmt.Sprintf("method has no handler: %s", envelope.Method), nil)
	}
	return handler(ctx, envelope)
}

func (s *Server) buildWelcome(params map[string]any) (map[string]any, error) {
	client, ok := params["client"].(map[string]any)
	if !ok {
		return nil, NewError("schema_invalid", "daemon.hello requires a client object", nil)
	}
	if !containsInt(client["supported_envelope"], SupportedEnvelopeVersion) {
		return nil, NewError("version_incompatible", "client and daemon have no shared envelope version", map[string]any{
			"daemon_supported_envelope": []int{SupportedEnvelopeVersion},
		})
	}
	if !containsString(client["supported_framings"], DefaultFraming) {
		return nil, NewError("version_incompatible", "client and daemon have no shared framing", map[string]any{
			"daemon_supported_framings": []string{DefaultFraming},
		})
	}
	return map[string]any{
		"daemon_version":   s.DaemonVersion,
		"envelope":         SupportedEnvelopeVersion,
		"framing":          DefaultFraming,
		"substrate":        "postgres",
		"substrate_schema": s.SubstrateSchema,
		"methods_etag":     MethodsETag(),
		"sealed_apply":     s.sealedApplyStatus(),
	}, nil
}

func (s *Server) sealedApplyStatus() map[string]any {
	if s.SealedApplyFunc != nil {
		status := s.SealedApplyFunc()
		if status != nil {
			return status
		}
	}
	return s.SealedApply
}

func (s *Server) markRequest(requestID string) bool {
	return s.seenRequests.Add(requestID)
}

func (s *Server) markHandshake(connectionID string) {
	s.handshakeSeen.Add(connectionID)
}

func (s *Server) hasHandshake(connectionID string) bool {
	return s.handshakeSeen.Contains(connectionID)
}

func repositoryID(params map[string]any) string {
	if value, ok := params["repository_id"]; ok && value != nil {
		return fmt.Sprint(value)
	}
	return ""
}

func deniedAuth(auth AuthContext, reason string) AuthContext {
	auth.Decision = "denied"
	auth.DenialReason = reason
	return auth
}

func containsInt(value any, expected int) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if intValue, ok := intValue(item); ok && intValue == expected {
			return true
		}
	}
	return false
}

func containsString(value any, expected string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && text == expected {
			return true
		}
	}
	return false
}

func stripSpace(value []byte) []byte {
	start := 0
	end := len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\r' || value[start] == '\n') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\r' || value[end-1] == '\n') {
		end--
	}
	return value[start:end]
}
