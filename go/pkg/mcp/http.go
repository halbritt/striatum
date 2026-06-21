package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/halbritt/striatum/go/pkg/rpc"
)

const (
	EndpointPath      = "/mcp"
	SSEEndpointPath   = "/mcp/sse"
	MessagePath       = "/mcp/messages"
	protocolVersion   = "2024-11-05"
	serverName        = "striatum-local"
	defaultAllowValue = "GET, POST, OPTIONS"

	// HeaderMCPSessionID is the streamable-HTTP session-correlation header (MCP
	// 2025-03-26 / 2025-06-18). #557: codex's spec-compliant streamable-HTTP MCP
	// client treats the server as "not initialized" — and falls back to a
	// hand-rolled HTTP path that intermittently fails to claim — unless the
	// `initialize` response carries an `Mcp-Session-Id` header it can echo on
	// every subsequent request. The daemon previously served the legacy
	// 2024-11-05 HTTP+SSE transport with NO session header, so codex lanes
	// registered a session but never claimed their work packet. We now mint a
	// session id on `initialize` and echo it back. The id is MCP transport
	// correlation, NOT a second auth factor — the bearer remains the only auth —
	// so a request that OMITS the header is still accepted (claude's existing
	// HTTP+SSE client never sends one; backward-compat is mandatory).
	HeaderMCPSessionID = "Mcp-Session-Id"

	// maxIssuedSessions bounds the in-memory issued-session set so a long-lived
	// daemon that mints a session per lane initialize cannot grow it without
	// limit. The set is lenient correlation state only: when it is full the
	// oldest-issued ids are evicted, and an evicted (or never-issued) id is NOT
	// hard-rejected — the bearer still governs auth — so eviction can never wedge
	// a reconnecting client.
	maxIssuedSessions = 4096

	jsonrpcVersion = "2.0"

	// HeaderBootEpoch is the request header a lane's MCP client presents to
	// carry the boot epoch of the daemon it was launched against (#316,
	// follow-up to #296). It is alias-agnostic on purpose: the literal alias
	// "striatum" already appears in tool names, the bootstrap prompt, generated
	// skills, and doctor/recovery name-matching, so the recycled-port defense
	// rides a single opaque header the lane simply echoes — NOT a literal
	// threaded across tool identities. The daemon compares the presented value
	// to its own live boot epoch and rejects a mismatch (StaleDaemonIdentityCode)
	// before the request can touch run state. The header value mirrors the
	// STRIATUM_MCP_BOOT_EPOCH env var the supervisor injects into the lane.
	HeaderBootEpoch = "X-Striatum-Boot-Epoch"

	// StaleDaemonIdentityCode is the distinct, machine-readable rejection code a
	// request gets when its presented boot epoch does not match the live
	// daemon's (#316). It is distinct from a generic MCP outage so recovery
	// requeues/relaunches the lane against the CURRENT daemon rather than
	// reporting a transport failure.
	StaleDaemonIdentityCode = "stale_daemon_identity"
)

// supportedProtocolVersions is the server-supported MCP protocol-version set,
// newest first. #557: `initialize` echoes the CLIENT's requested
// params.protocolVersion when it appears here (so a 2025-06-18 streamable-HTTP
// client like codex 0.140 negotiates 2025-06-18, and a legacy 2024-11-05 client
// like claude's HTTP+SSE path still gets 2024-11-05 unchanged); a client that
// requests an unsupported (or no) version gets latestProtocolVersion. 2024-11-05
// MUST stay in this set — dropping it would break claude's existing client.
var supportedProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// latestProtocolVersion is the version returned when the client requests one the
// server does not support (or requests none). It is the first (newest) entry of
// supportedProtocolVersions.
const latestProtocolVersion = "2025-06-18"

func negotiateProtocolVersion(requested string) string {
	for _, supported := range supportedProtocolVersions {
		if requested == supported {
			return requested
		}
	}
	return latestProtocolVersion
}

const (
	jsonrpcParseError     = -32700
	jsonrpcInvalidRequest = -32600
	jsonrpcMethodNotFound = -32601
	jsonrpcInvalidParams  = -32602
	jsonrpcInternalError  = -32603
	jsonrpcAuthError      = -32001
	jsonrpcForbidden      = -32003
)

type HTTPHandler struct {
	Service       Service
	ServerName    string
	ServerVersion string

	mu       sync.Mutex
	sessions map[string]*sseSession

	// issuedMu guards the streamable-HTTP issued-session set (#557). It is
	// separate from `mu` (which guards the SSE `sessions` map) so minting/looking
	// up a streamable-HTTP session id never contends with an active SSE stream.
	issuedMu       sync.Mutex
	issuedSessions map[string]struct{}
	issuedOrder    []string
}

type sseSession struct {
	id       string
	messages chan []byte
}

type jsonrpcResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *jsonrpcError  `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func NewHTTPHandler(service Service) *HTTPHandler {
	return &HTTPHandler{
		Service:        service,
		ServerName:     serverName,
		ServerVersion:  serverVersion(service),
		sessions:       map[string]*sseSession{},
		issuedSessions: map[string]struct{}{},
	}
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isSupportedPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}
	if localErr := validateLocalRequest(r); localErr != nil {
		writeJSONResponseStatus(w, http.StatusForbidden, errorResponse(nil, jsonrpcForbidden, localErr.Message, errorData(localErr.Code, nil)))
		return
	}
	// #316: reject a request that authenticated against a RECYCLED port now
	// bound by a DIFFERENT live daemon process run. The check runs at this early
	// layer — before bearer validation, before dispatch — so a stale lane can
	// never touch another active run's workflow state. Backward-compatible: a
	// request that presents NO epoch is allowed (lanes launched before #316
	// carry none); enforcement only fires for a request that presents an epoch
	// that disagrees with the live daemon's.
	if epochErr := h.validateBootEpoch(r); epochErr != nil {
		writeJSONResponseStatus(w, http.StatusForbidden, errorResponse(nil, jsonrpcForbidden, epochErr.Message, errorData(epochErr.Code, nil)))
		return
	}
	setLocalCORSHeaders(w, r)
	if isEndpointPath(r.URL.Path) && r.Method == http.MethodGet {
		h.serveStream(w, r)
		return
	}
	if (isEndpointPath(r.URL.Path) || r.URL.Path == MessagePath) && r.Method == http.MethodPost {
		h.servePost(w, r)
		return
	}
	if r.Method == http.MethodOptions {
		if isEndpointPath(r.URL.Path) {
			w.Header().Set("Allow", defaultAllowValue)
		} else {
			w.Header().Set("Allow", "POST, OPTIONS")
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if isEndpointPath(r.URL.Path) {
		w.Header().Set("Allow", defaultAllowValue)
	} else {
		w.Header().Set("Allow", "POST, OPTIONS")
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (h *HTTPHandler) serveStream(w http.ResponseWriter, r *http.Request) {
	if _, rpcErr := bearerToken(r); rpcErr != nil {
		writeJSONResponseStatus(w, http.StatusUnauthorized, jsonrpcResponse{JSONRPC: jsonrpcVersion, ID: nil, Error: rpcErr})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	session := h.newSession()
	defer h.closeSession(session.id)

	writeSSEHeaders(w)
	if err := writeSSEEvent(w, "endpoint", h.endpointURL(r, session.id)); err != nil {
		return
	}
	flusher.Flush()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case payload := <-session.messages:
			if err := writeSSEEvent(w, "message", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (h *HTTPHandler) servePost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, rpc.MaxEnvelopeBytes))
	if err != nil {
		response := errorResponse(nil, jsonrpcInvalidRequest, err.Error(), errorData("malformed_body", nil))
		writeJSONResponse(w, response)
		return
	}
	response, respond, issuedSessionID := h.handleBody(r.Context(), r, body)
	// #557: streamable-HTTP session correlation. handleBody mints a session id on
	// an `initialize` request; surface it as the Mcp-Session-Id response header
	// BEFORE writing the body so a spec-compliant client (codex) can echo it on
	// subsequent requests. The header is harmless to clients that ignore it
	// (claude's HTTP+SSE path).
	if issuedSessionID != "" {
		w.Header().Set(HeaderMCPSessionID, issuedSessionID)
	}
	if !respond {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		writeJSONResponse(w, errorResponse(nil, jsonrpcInternalError, err.Error(), nil))
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSONPayload(w, encoded)
		return
	}
	session := h.session(sessionID)
	if session == nil {
		http.Error(w, "unknown MCP SSE session", http.StatusNotFound)
		return
	}
	select {
	case session.messages <- encoded:
		w.WriteHeader(http.StatusAccepted)
	case <-r.Context().Done():
		http.Error(w, r.Context().Err().Error(), http.StatusRequestTimeout)
	}
}

// handleBody parses and dispatches a single JSON-RPC request. The third return
// value is the streamable-HTTP session id minted for an `initialize` request
// (#557); it is empty for every other method and for the parse/validation error
// paths. servePost surfaces it as the Mcp-Session-Id response header.
func (h *HTTPHandler) handleBody(ctx context.Context, r *http.Request, body []byte) (jsonrpcResponse, bool, string) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return errorResponse(nil, jsonrpcParseError, "invalid JSON: "+err.Error(), errorData("malformed_body", nil)), true, ""
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errorResponse(nil, jsonrpcParseError, "request body must contain one JSON-RPC object", errorData("malformed_body", nil)), true, ""
	}
	if payload == nil {
		return errorResponse(nil, jsonrpcInvalidRequest, "JSON-RPC request must be an object", errorData("schema_invalid", nil)), true, ""
	}
	rawID, hasID := payload["id"]
	requestID, validID := jsonrpcID(rawID)
	if !validID {
		return errorResponse(nil, jsonrpcInvalidRequest, "id must be a string, number, or null", errorData("schema_invalid", nil)), true, ""
	}
	if payload["jsonrpc"] != jsonrpcVersion {
		return errorResponse(requestID, jsonrpcInvalidRequest, "jsonrpc must be '2.0'", errorData("schema_invalid", nil)), true, ""
	}
	method, ok := payload["method"].(string)
	if !ok || method == "" {
		return errorResponse(requestID, jsonrpcInvalidRequest, "method is required", errorData("schema_invalid", nil)), true, ""
	}
	params, err := requestParams(payload)
	if err != nil {
		return errorResponse(requestID, jsonrpcInvalidParams, err.Error(), errorData("schema_invalid", nil)), true, ""
	}

	// #557: mint a streamable-HTTP session id for an `initialize` request and
	// negotiate the protocol version from the client's request. The id is
	// recorded in the lenient issued-set and surfaced to servePost so it lands on
	// the Mcp-Session-Id response header. Non-initialize methods get no id.
	var issuedSessionID string
	if method == "initialize" {
		issuedSessionID = h.newIssuedSession()
	}

	result, rpcErr := h.dispatch(ctx, r, requestID, method, params)
	if !hasID {
		return jsonrpcResponse{}, false, issuedSessionID
	}
	if rpcErr != nil {
		return jsonrpcResponse{JSONRPC: jsonrpcVersion, ID: requestID, Error: rpcErr}, true, issuedSessionID
	}
	return jsonrpcResponse{JSONRPC: jsonrpcVersion, ID: requestID, Result: result}, true, issuedSessionID
}

func (h *HTTPHandler) dispatch(ctx context.Context, r *http.Request, requestID any, method string, params map[string]any) (map[string]any, *jsonrpcError) {
	switch method {
	case "initialize":
		requestedVersion, _ := params["protocolVersion"].(string)
		return h.initializeResult(negotiateProtocolVersion(requestedVersion)), nil
	case "notifications/initialized":
		return map[string]any{}, nil
	case "tools/list":
		token, rpcErr := bearerToken(r)
		if rpcErr != nil {
			return nil, rpcErr
		}
		if _, err := optionalString(params, "repository_id"); err != nil {
			return nil, invalidParams(err)
		}
		return h.Service.ToolsList(ctx, params, token), nil
	case "tools/call":
		name, err := requiredString(params, "name")
		if err != nil {
			return nil, invalidParams(err)
		}
		arguments, err := objectParam(params, "arguments")
		if err != nil {
			return nil, invalidParams(err)
		}
		repositoryID, err := optionalString(params, "repository_id")
		if err != nil {
			return nil, invalidParams(err)
		}
		if repositoryID != "" {
			if _, exists := arguments["repository_id"]; !exists {
				arguments["repository_id"] = repositoryID
			}
		}
		token, rpcErr := bearerToken(r)
		if rpcErr != nil {
			return nil, rpcErr
		}
		callRequestID, err := callRequestID(params, requestID)
		if err != nil {
			return nil, invalidParams(err)
		}
		return h.Service.ToolsCall(ctx, name, arguments, token, callRequestID), nil
	default:
		return nil, &jsonrpcError{Code: jsonrpcMethodNotFound, Message: fmt.Sprintf("unknown method %q", method), Data: errorData("method_unknown", nil)}
	}
}

func (h *HTTPHandler) initializeResult(negotiatedVersion string) map[string]any {
	name := h.ServerName
	if name == "" {
		name = serverName
	}
	version := h.ServerVersion
	if version == "" {
		version = serverVersion(h.Service)
	}
	if negotiatedVersion == "" {
		negotiatedVersion = protocolVersion
	}
	return map[string]any{
		"protocolVersion": negotiatedVersion,
		"serverInfo": map[string]any{
			"name":    name,
			"version": version,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
}

func (h *HTTPHandler) newSession() *sseSession {
	session := &sseSession{
		id:       newSessionID(),
		messages: make(chan []byte, 8),
	}
	h.mu.Lock()
	h.sessions[session.id] = session
	h.mu.Unlock()
	return session
}

func (h *HTTPHandler) closeSession(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

func (h *HTTPHandler) session(id string) *sseSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessions[id]
}

// newIssuedSession mints a streamable-HTTP session id (#557), records it in the
// bounded, lenient issued-set, and returns it. The set is FIFO-evicted past
// maxIssuedSessions so a long-lived daemon cannot grow it without limit;
// eviction is harmless because isIssuedSession is advisory only — the bearer is
// the auth boundary, and an unknown/evicted id is never hard-rejected.
func (h *HTTPHandler) newIssuedSession() string {
	id := newSessionID()
	h.issuedMu.Lock()
	defer h.issuedMu.Unlock()
	if h.issuedSessions == nil {
		h.issuedSessions = map[string]struct{}{}
	}
	if _, exists := h.issuedSessions[id]; !exists {
		h.issuedSessions[id] = struct{}{}
		h.issuedOrder = append(h.issuedOrder, id)
		for len(h.issuedOrder) > maxIssuedSessions {
			oldest := h.issuedOrder[0]
			h.issuedOrder = h.issuedOrder[1:]
			delete(h.issuedSessions, oldest)
		}
	}
	return id
}

// isIssuedSession reports whether id is one this handler minted. It is advisory:
// callers MUST NOT hard-reject an unknown id, because the bearer is the real
// auth and a client that reconnects (or a request that omits the header) must
// still be served. It exists only so diagnostics/future correlation can tell an
// echoed-back id from a forged one without changing the request's fate.
func (h *HTTPHandler) isIssuedSession(id string) bool {
	if id == "" {
		return false
	}
	h.issuedMu.Lock()
	defer h.issuedMu.Unlock()
	_, ok := h.issuedSessions[id]
	return ok
}

func (h *HTTPHandler) endpointURL(r *http.Request, sessionID string) string {
	return MessagePath + "?session_id=" + sessionID
}

func requestParams(payload map[string]any) (map[string]any, error) {
	value, exists := payload["params"]
	if !exists || value == nil {
		return map[string]any{}, nil
	}
	params, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("params must be an object")
	}
	return cloneMap(params), nil
}

func requiredString(params map[string]any, key string) (string, error) {
	value, exists := params[key]
	if !exists || value == nil {
		return "", fmt.Errorf("%s is required", key)
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return "", fmt.Errorf("%s must be a non-empty string", key)
	}
	return text, nil
}

func optionalString(params map[string]any, key string) (string, error) {
	value, exists := params[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return text, nil
}

func objectParam(params map[string]any, key string) (map[string]any, error) {
	value, exists := params[key]
	if !exists || value == nil {
		return map[string]any{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return cloneMap(object), nil
}

func bearerToken(r *http.Request) (string, *jsonrpcError) {
	header := r.Header.Get("Authorization")
	if strings.TrimSpace(header) == "" {
		return "", &jsonrpcError{Code: jsonrpcAuthError, Message: "Authorization bearer token is required", Data: errorData("token_missing", nil)}
	}
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", &jsonrpcError{Code: jsonrpcAuthError, Message: "Authorization must use Bearer token syntax", Data: errorData("token_malformed", nil)}
	}
	return parts[1], nil
}

func callRequestID(params map[string]any, jsonrpcID any) (string, error) {
	requestID, err := optionalString(params, "request_id")
	if err != nil {
		return "", err
	}
	if requestID != "" {
		return requestID, nil
	}
	switch value := jsonrpcID.(type) {
	case string:
		if value != "" {
			return "mcp_" + newSessionID() + "_" + value, nil
		}
	case json.Number:
		if value.String() != "" {
			return "mcp_" + newSessionID() + "_" + value.String(), nil
		}
	case nil:
	}
	return "mcp_" + newSessionID(), nil
}

func cloneMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func jsonrpcID(value any) (any, bool) {
	if value == nil {
		return nil, true
	}
	switch v := value.(type) {
	case string, json.Number:
		return v, true
	default:
		return nil, false
	}
}

func invalidParams(err error) *jsonrpcError {
	return &jsonrpcError{Code: jsonrpcInvalidParams, Message: err.Error(), Data: errorData("schema_invalid", nil)}
}

func errorResponse(id any, code int, message string, data any) jsonrpcResponse {
	return jsonrpcResponse{
		JSONRPC: jsonrpcVersion,
		ID:      id,
		Error:   &jsonrpcError{Code: code, Message: message, Data: data},
	}
}

func writeJSONResponse(w http.ResponseWriter, response jsonrpcResponse) {
	writeJSONResponseStatus(w, http.StatusOK, response)
}

func writeJSONResponseStatus(w http.ResponseWriter, status int, response jsonrpcResponse) {
	encoded, err := json.Marshal(response)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONPayloadStatus(w, status, encoded)
}

func writeJSONPayload(w http.ResponseWriter, payload []byte) {
	writeJSONPayloadStatus(w, http.StatusOK, payload)
}

func writeJSONPayloadStatus(w http.ResponseWriter, status int, payload []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(payload)
}

func writeSSEHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

func writeSSEEvent(w io.Writer, event string, data any) error {
	var payload []byte
	switch value := data.(type) {
	case []byte:
		payload = value
	case string:
		payload = []byte(value)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		payload = encoded
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	for _, line := range strings.Split(string(payload), "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func newSessionID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func serverVersion(service Service) string {
	if service.RPC == nil || service.RPC.DaemonVersion == "" {
		return "0.1.0"
	}
	return service.RPC.DaemonVersion
}

type localRequestError struct {
	Code    string
	Message string
}

func isSupportedPath(path string) bool {
	return isEndpointPath(path) || path == MessagePath
}

func isEndpointPath(path string) bool {
	return path == EndpointPath || path == SSEEndpointPath
}

// validateBootEpoch enforces the #316 recycled-port identity check. It returns
// a localRequestError (rendered as the distinct StaleDaemonIdentityCode) when
// the request presents a HeaderBootEpoch that disagrees with the live daemon's
// boot epoch. It is deliberately permissive in two directions to stay additive:
//
//   - When the handler holds no live BootEpoch (e.g. a daemon build/path that
//     did not mint one, or a unit-test handler), nothing is enforced.
//   - When the request presents NO HeaderBootEpoch, it is allowed (logged at
//     the call layer is unnecessary noise; the absence is the expected shape for
//     any lane launched before #316). The protection therefore fires only for a
//     lane that carries an epoch AND that epoch is wrong — exactly the recycled
//     -port / stale-pin case, where the presented epoch is some OTHER daemon's.
func (h *HTTPHandler) validateBootEpoch(r *http.Request) *localRequestError {
	live := strings.TrimSpace(h.Service.BootEpoch)
	if live == "" {
		return nil
	}
	presented := strings.TrimSpace(r.Header.Get(HeaderBootEpoch))
	if presented == "" {
		return nil
	}
	if presented == live {
		return nil
	}
	return &localRequestError{
		// The literal here (not the StaleDaemonIdentityCode const) is what the
		// rpc error-catalog reverse-reconciliation guard scans for; keep them in
		// sync — staleDaemonIdentityCodeMatchesCatalog (http_test.go) pins it.
		Code:    "stale_daemon_identity",
		Message: "request presents a stale daemon boot epoch; the MCP port was recycled by a different daemon process run — relaunch the lane against the current daemon",
	}
}

func validateLocalRequest(r *http.Request) *localRequestError {
	if !isLoopbackHost(r.Host) {
		return &localRequestError{Code: "bad_host", Message: "Host must be loopback"}
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" && !isLoopbackOrigin(origin) {
		return &localRequestError{Code: "bad_origin", Message: "Origin must be loopback"}
	}
	return nil
}

func setLocalCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept")
	header.Set("Access-Control-Allow-Methods", defaultAllowValue)
	header.Add("Vary", "Origin")
}

func isLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return isLoopbackHost(parsed.Host)
}

func isLoopbackHost(value string) bool {
	host := normalizeHost(value)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		return strings.Trim(value, "[]")
	}
	return value
}

func errorData(code string, details any) map[string]any {
	data := map[string]any{"code": code}
	if details != nil {
		data["details"] = details
	}
	return data
}
