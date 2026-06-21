package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/halbritt/striatum/go/pkg/sessionliveness"
)

func TestHTTPHandlerInitializeDirectPost(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}}`, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	if response.ID != "init" || response.Error != nil {
		t.Fatalf("initialize response = %#v", response)
	}
	// #557: a request that names NO protocolVersion negotiates the server's
	// latest supported version (it is treated like an unsupported request).
	if response.Result["protocolVersion"] != latestProtocolVersion {
		t.Fatalf("protocolVersion = %#v, want latest %q", response.Result["protocolVersion"], latestProtocolVersion)
	}
}

// #557: the `initialize` POST response carries a non-empty Mcp-Session-Id header
// so a spec-compliant streamable-HTTP client (codex) considers the server
// initialized and stops falling back to a hand-rolled HTTP path that intermittently
// failed to claim work.
func TestHTTPHandlerInitializeReturnsSessionIDHeader(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	sessionID := recorder.Header().Get(HeaderMCPSessionID)
	if sessionID == "" {
		t.Fatalf("initialize response missing %s header; headers=%#v", HeaderMCPSessionID, recorder.Header())
	}
	if !handler.isIssuedSession(sessionID) {
		t.Fatalf("issued session id %q not recorded in the issued-set", sessionID)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json (codex accepts JSON for a single initialize response)", got)
	}
}

// #557: protocol-version negotiation. A client that requests a supported version
// gets exactly that version back (so 2025-06-18 streamable-HTTP and legacy
// 2024-11-05 HTTP+SSE both negotiate their own); a client requesting an
// unsupported version gets the server's latest supported version.
func TestHTTPHandlerInitializeNegotiatesProtocolVersion(t *testing.T) {
	cases := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "streamable-http 2025-06-18 echoed", requested: "2025-06-18", want: "2025-06-18"},
		{name: "interim 2025-03-26 echoed", requested: "2025-03-26", want: "2025-03-26"},
		{name: "legacy 2024-11-05 preserved (claude path)", requested: "2024-11-05", want: "2024-11-05"},
		{name: "unsupported version falls back to latest", requested: "1999-01-01", want: latestProtocolVersion},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, _, _, _ := newTestHTTPHandler(t)
			body := `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"` + tc.requested + `"}}`
			recorder := postJSON(t, handler, EndpointPath, body, "")
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			response := decodeTestResponse(t, recorder)
			if response.Error != nil {
				t.Fatalf("initialize error = %#v", response.Error)
			}
			if response.Result["protocolVersion"] != tc.want {
				t.Fatalf("requested %q -> protocolVersion %#v, want %q", tc.requested, response.Result["protocolVersion"], tc.want)
			}
			// every initialize, regardless of negotiated version, still mints a
			// session-id header (the legacy 2024-11-05 client simply ignores it).
			if recorder.Header().Get(HeaderMCPSessionID) == "" {
				t.Fatalf("requested %q: missing %s header", tc.requested, HeaderMCPSessionID)
			}
		})
	}
}

// #557 backward-compat / streamable-HTTP correlation: a subsequent POST that
// ECHOES the issued Mcp-Session-Id succeeds, AND a subsequent POST that OMITS it
// ALSO succeeds (claude's HTTP+SSE client never sends one — the bearer is the
// only auth, the session id is transport correlation, not a second auth factor).
func TestHTTPHandlerSubsequentPostAcceptsWithAndWithoutSessionID(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)

	// initialize to obtain a session id.
	initRecorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"init","method":"initialize","params":{"protocolVersion":"2025-06-18"}}`, "")
	sessionID := initRecorder.Header().Get(HeaderMCPSessionID)
	if sessionID == "" {
		t.Fatalf("initialize did not return a session id")
	}

	// subsequent tools/list WITH the issued session id -> success.
	withReq := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")
	withReq.Header.Set(HeaderMCPSessionID, sessionID)
	withRec := httptest.NewRecorder()
	handler.ServeHTTP(withRec, withReq)
	if withRec.Code != http.StatusOK {
		t.Fatalf("tools/list WITH session id status = %d, body = %s", withRec.Code, withRec.Body.String())
	}
	if resp := decodeTestResponse(t, withRec); resp.Error != nil {
		t.Fatalf("tools/list WITH session id error = %#v", resp.Error)
	}

	// subsequent tools/list WITHOUT the session id -> still success (claude path).
	withoutRec := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")
	if withoutRec.Code != http.StatusOK {
		t.Fatalf("tools/list WITHOUT session id status = %d, body = %s", withoutRec.Code, withoutRec.Body.String())
	}
	if resp := decodeTestResponse(t, withoutRec); resp.Error != nil {
		t.Fatalf("tools/list WITHOUT session id error = %#v", resp.Error)
	}

	// an UNKNOWN/forged session id must NOT hard-reject the request — the bearer
	// is the auth boundary, and a reconnecting client may present an id we no
	// longer hold.
	unknownReq := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")
	unknownReq.Header.Set(HeaderMCPSessionID, "deadbeefdeadbeefdeadbeefdeadbeef")
	unknownRec := httptest.NewRecorder()
	handler.ServeHTTP(unknownRec, unknownReq)
	if unknownRec.Code != http.StatusOK {
		t.Fatalf("tools/list with UNKNOWN session id status = %d (must not hard-reject), body = %s", unknownRec.Code, unknownRec.Body.String())
	}
}

func TestHTTPHandlerToolsListUsesBearerTokenAndHidesUnauthorized(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	if response.Error != nil {
		t.Fatalf("tools/list error = %#v", response.Error)
	}
	tools, ok := response.Result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %#v", response.Result)
	}
	names := toolNames(tools)
	if !names["status"] {
		t.Fatalf("read token did not see status tool: %#v", names)
	}
	if !names["git.snapshot"] {
		t.Fatalf("read token did not see git.snapshot tool: %#v", names)
	}
	for _, hidden := range []string{"work.complete", "workflow.generate"} {
		if names[hidden] {
			t.Fatalf("read token saw unauthorized/hidden tool %s: %#v", hidden, names)
		}
	}
}

func TestHTTPHandlerToolsListWithoutRepositoryIDAdvertisesLaneTools(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`, "lane.secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	if response.Error != nil {
		t.Fatalf("tools/list error = %#v", response.Error)
	}
	tools, ok := response.Result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("parameterless tools/list returned no tools for lane token: %#v", response.Result)
	}
	names := toolNames(tools)
	for _, want := range []string{"work.await_packet", "work.ack", "artifact.publish", "review.submit"} {
		if !names[want] {
			t.Fatalf("lane token tools/list missing %s: %#v", want, names)
		}
	}
}

func TestHTTPHandlerToolsListRecordsSessionActivity(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := &activityRecorder{}
	handler.Service.ActivityRecorder = recorder
	body := `{"jsonrpc":"2.0","id":"list-activity","method":"tools/list","params":{"repository_id":"repo_1","session_id":"sess_1"}}`

	responseRecorder := postJSON(t, handler, EndpointPath, body, "read.secret")
	response := decodeTestResponse(t, responseRecorder)
	if response.Error != nil {
		t.Fatalf("tools/list error = %#v", response.Error)
	}
	if recorder.repositoryID != "repo_1" || recorder.sessionID != "sess_1" {
		t.Fatalf("activity scope = repo %q session %q", recorder.repositoryID, recorder.sessionID)
	}
	if len(recorder.columns) != 1 || recorder.columns[0] != sessionliveness.LastToolsListAt {
		t.Fatalf("activity columns = %#v", recorder.columns)
	}
}

func TestHTTPHandlerToolsCallReadPath(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"read","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "read.secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if structured["ok"] != true || structured["method"] != "status" || response.Result["isError"] != false {
		t.Fatalf("read tool call result = %#v", response.Result)
	}
	data, ok := structured["data"].(map[string]any)
	if !ok || data["status"] != "ok" {
		t.Fatalf("read tool data = %#v", structured["data"])
	}
}

type activityRecorder struct {
	repositoryID string
	sessionID    string
	columns      []string
	// calls accumulates every RecordSessionActivity invocation so a test can
	// assert an ordered boundary pair (tool-call start then finish, #83).
	calls []recordedActivity
}

type recordedActivity struct {
	repositoryID string
	sessionID    string
	columns      []string
}

func (r *activityRecorder) RecordSessionActivity(_ context.Context, repositoryID string, sessionID string, columns ...string) error {
	r.repositoryID = repositoryID
	r.sessionID = sessionID
	r.columns = append([]string(nil), columns...)
	r.calls = append(r.calls, recordedActivity{
		repositoryID: repositoryID,
		sessionID:    sessionID,
		columns:      append([]string(nil), columns...),
	})
	return nil
}

// TestHTTPHandlerToolsCallRecordsToolCallBoundary guards RFC 0101 Phase 1
// (#83): a session-scoped tools/call stamps last_tool_call_started_at before
// dispatch and last_tool_call_finished_at after it returns, so the liveness
// classifier can report working_tool with a visible since/deadline while a lane
// is inside a hidden MCP call. Only the boundary timing is recorded, never tool
// content (D028).
func TestHTTPHandlerToolsCallRecordsToolCallBoundary(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := &activityRecorder{}
	handler.Service.ActivityRecorder = recorder
	body := `{"jsonrpc":"2.0","id":"read","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1","session_id":"sess_1"}}}`

	responseRecorder := postJSON(t, handler, EndpointPath, body, "read.secret")
	response := decodeTestResponse(t, responseRecorder)
	if response.Error != nil {
		t.Fatalf("tools/call error = %#v", response.Error)
	}

	var started, finished int
	startedIndex, finishedIndex := -1, -1
	for i, call := range recorder.calls {
		if call.repositoryID != "repo_1" || call.sessionID != "sess_1" {
			t.Fatalf("call %d scope = repo %q session %q", i, call.repositoryID, call.sessionID)
		}
		for _, column := range call.columns {
			switch column {
			case sessionliveness.LastToolCallStartedAt:
				started++
				startedIndex = i
			case sessionliveness.LastToolCallFinishedAt:
				finished++
				finishedIndex = i
			}
		}
	}
	if started != 1 || finished != 1 {
		t.Fatalf("want exactly one start and one finish; got start=%d finish=%d calls=%#v", started, finished, recorder.calls)
	}
	if startedIndex >= finishedIndex {
		t.Fatalf("start must be recorded before finish; startIndex=%d finishIndex=%d", startedIndex, finishedIndex)
	}
}

// TestHTTPHandlerToolsCallUnscopedSkipsBoundary asserts a tools/call without a
// session_id records no tool-call boundary (the recorder is a no-op when the
// scope is absent), so anonymous/unscoped calls are unaffected.
func TestHTTPHandlerToolsCallUnscopedSkipsBoundary(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := &activityRecorder{}
	handler.Service.ActivityRecorder = recorder
	body := `{"jsonrpc":"2.0","id":"read","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`

	postJSON(t, handler, EndpointPath, body, "read.secret")
	if len(recorder.calls) != 0 {
		t.Fatalf("unscoped tools/call must record no activity; got %#v", recorder.calls)
	}
}

func TestHTTPHandlerToolsCallMutationPath(t *testing.T) {
	handler, mutationCalled, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"write","method":"tools/call","params":{"name":"work.complete","repository_id":"repo_1","arguments":{"session_id":"sess_1","job_id":"job_1","lease_id":"lease_1"}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "write.secret")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !*mutationCalled {
		t.Fatal("work.complete handler was not called")
	}
	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if structured["ok"] != true || structured["method"] != "work.complete" || response.Result["isError"] != false {
		t.Fatalf("mutation tool call result = %#v", response.Result)
	}
	data, ok := structured["data"].(map[string]any)
	if !ok || data["mutated"] != true {
		t.Fatalf("mutation tool data = %#v", structured["data"])
	}
}

func TestHTTPHandlerToolsCallInvalidTokenDenies(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"invalid-token","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "missing.secret")

	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if response.Result["isError"] != true || structured["error"] != "token_invalid" {
		t.Fatalf("invalid token result = %#v", response.Result)
	}
}

func TestHTTPHandlerWriteTokenCannotCallReadOnlyGitSnapshot(t *testing.T) {
	handler, _, _, gitSnapshotCalled := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"git-denied","method":"tools/call","params":{"name":"git.snapshot","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "write.secret")

	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if response.Result["isError"] != true || structured["error"] != "capability_missing" {
		t.Fatalf("git.snapshot denial result = %#v", response.Result)
	}
	if *gitSnapshotCalled {
		t.Fatal("git.snapshot handler ran for write-only token")
	}
}

func TestHTTPHandlerDeniedTokenCannotCallHiddenUnauthorizedMethod(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"hidden-denied","method":"tools/call","params":{"name":"workflow.generate","repository_id":"repo_1","arguments":{}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "read.secret")

	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if response.Result["isError"] != true || structured["error"] != "tool_hidden" {
		t.Fatalf("hidden unauthorized method result = %#v", response.Result)
	}
}

func TestHTTPHandlerWriteTokenCannotCallHiddenProductionTool(t *testing.T) {
	handler, _, hiddenCalled, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"hidden-write-denied","method":"tools/call","params":{"name":"workflow.generate","repository_id":"repo_1","arguments":{}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "write.secret")

	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if response.Result["isError"] != true || structured["error"] != "tool_hidden" {
		t.Fatalf("hidden write-capable method result = %#v", response.Result)
	}
	if *hiddenCalled {
		t.Fatal("workflow.generate handler ran despite hidden production MCP denial")
	}
}

func TestHTTPHandlerToolsCallUnknownDaemonMethodReturnsMCPError(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"unknown-daemon","method":"tools/call","params":{"name":"not.a.method","arguments":{"repository_id":"repo_1"}}}`
	recorder := postJSON(t, handler, EndpointPath, body, "read.secret")

	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if response.Result["isError"] != true || structured["error"] != "method_unknown" {
		t.Fatalf("unknown daemon method result = %#v", response.Result)
	}
}

func TestHTTPHandlerMissingAuthReturnsJSONRPCError(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "")

	response := decodeTestResponse(t, recorder)
	if response.Error == nil || response.Error.Code != jsonrpcAuthError {
		t.Fatalf("missing auth error = %#v", response.Error)
	}
	assertErrorDataCode(t, response.Error, "token_missing")
}

func TestHTTPHandlerRejectsBadOrigin(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")
	request.Header.Set("Origin", "https://example.invalid")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	assertErrorDataCode(t, response.Error, "bad_origin")
}

func TestHTTPHandlerRejectsBadHost(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{"repository_id":"repo_1"}}`, "read.secret")
	request.Host = "example.invalid"
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	assertErrorDataCode(t, response.Error, "bad_host")
}

// #316: a request that presents the LIVE daemon boot epoch is accepted (the
// epoch check passes through and the call dispatches normally).
func TestHTTPHandlerBootEpochLiveAccepted(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.BootEpoch = "epoch-live"
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{"name":"status","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`, "read.secret")
	request.Header.Set(HeaderBootEpoch, "epoch-live")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	structured := structuredContent(t, response)
	if structured["ok"] != true {
		t.Fatalf("live-epoch call not accepted: %#v", structured)
	}
}

// #316: a request that presents a DIFFERENT (stale) boot epoch is rejected with
// the distinct stale_daemon_identity code, BEFORE the request can touch run
// state (the dispatch never runs).
func TestHTTPHandlerBootEpochStaleRejected(t *testing.T) {
	handler, mutationCalled, _, _ := newTestHTTPHandler(t)
	handler.Service.BootEpoch = "epoch-live"
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"mut","method":"tools/call","params":{"name":"work.complete","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`, "write.secret")
	request.Header.Set(HeaderBootEpoch, "epoch-stale-other-daemon")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	response := decodeTestResponse(t, recorder)
	assertErrorDataCode(t, response.Error, StaleDaemonIdentityCode)
	if response.Error.Code != jsonrpcForbidden {
		t.Fatalf("rpc error code = %d, want forbidden %d", response.Error.Code, jsonrpcForbidden)
	}
	if *mutationCalled {
		t.Fatal("stale-epoch request reached the mutation handler; the check must run before dispatch")
	}
}

// #316 backward-compat: a request that presents NO boot epoch is accepted even
// when the daemon holds a live epoch (lanes launched before #316 carry none).
func TestHTTPHandlerBootEpochAbsentAccepted(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.BootEpoch = "epoch-live"
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{"name":"status","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`, "read.secret")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	structured := structuredContent(t, decodeTestResponse(t, recorder))
	if structured["ok"] != true {
		t.Fatalf("epoch-less call not accepted (backward-compat broken): %#v", structured)
	}
}

// #316: when the daemon holds NO live epoch (a build/path that did not mint
// one), even a present epoch header is ignored — the check is disabled.
func TestHTTPHandlerBootEpochDisabledWhenDaemonHasNone(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t) // BootEpoch left empty
	request := newJSONRequest(t, EndpointPath, `{"jsonrpc":"2.0","id":"status","method":"tools/call","params":{"name":"status","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`, "read.secret")
	request.Header.Set(HeaderBootEpoch, "epoch-whatever")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

// staleDaemonIdentityCodeMatchesCatalog pins the exported StaleDaemonIdentityCode
// const to the literal the handler emits and the rpc error catalog enumerates.
func TestStaleDaemonIdentityCodeMatchesCatalog(t *testing.T) {
	if StaleDaemonIdentityCode != "stale_daemon_identity" {
		t.Fatalf("StaleDaemonIdentityCode = %q, want stale_daemon_identity", StaleDaemonIdentityCode)
	}
	if _, ok := rpc.LookupErrorCode(StaleDaemonIdentityCode); !ok {
		t.Fatalf("StaleDaemonIdentityCode %q is not in the rpc error catalog", StaleDaemonIdentityCode)
	}
}

func TestHTTPHandlerMalformedBodyReturnsStableError(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{`, "read.secret")

	response := decodeTestResponse(t, recorder)
	if response.Error == nil || response.Error.Code != jsonrpcParseError {
		t.Fatalf("malformed body error = %#v", response.Error)
	}
	assertErrorDataCode(t, response.Error, "malformed_body")
}

func TestHTTPHandlerUnknownJSONRPCMethodReturnsStableError(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	recorder := postJSON(t, handler, EndpointPath, `{"jsonrpc":"2.0","id":"unknown","method":"resources/list","params":{}}`, "")

	response := decodeTestResponse(t, recorder)
	if response.Error == nil || response.Error.Code != jsonrpcMethodNotFound {
		t.Fatalf("unknown method error = %#v", response.Error)
	}
	assertErrorDataCode(t, response.Error, "method_unknown")
}

func TestHTTPHandlerStreamsMessageResponsesFromSSEAlias(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	streamRequest, err := http.NewRequest(http.MethodGet, server.URL+SSEEndpointPath+"?token=secret", nil)
	if err != nil {
		t.Fatalf("new stream request: %v", err)
	}
	streamRequest.Header.Set("Authorization", "Bearer read.secret")
	stream, err := server.Client().Do(streamRequest)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = stream.Body.Close() }()
	reader := bufio.NewReader(stream.Body)

	event, data := readTestSSEEvent(t, reader)
	if event != "endpoint" {
		t.Fatalf("first event = %q, want endpoint", event)
	}
	if !strings.HasPrefix(data, MessagePath+"?session_id=") {
		t.Fatalf("endpoint data = %q, want %s session endpoint", data, MessagePath)
	}
	if strings.Contains(data, "secret") {
		t.Fatalf("endpoint data leaked query token: %q", data)
	}

	payload := `{"jsonrpc":"2.0","id":"stream-list","method":"tools/list","params":{"repository_id":"repo_1"}}`
	postRequest, err := http.NewRequest(http.MethodPost, server.URL+data, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("new post request: %v", err)
	}
	postRequest.Header.Set("Authorization", "Bearer read.secret")
	postRequest.Header.Set("Content-Type", "application/json")
	postResponse, err := server.Client().Do(postRequest)
	if err != nil {
		t.Fatalf("post message: %v", err)
	}
	_ = postResponse.Body.Close()
	if postResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("post status = %d, want 202", postResponse.StatusCode)
	}

	event, data = readTestSSEEvent(t, reader)
	if event != "message" {
		t.Fatalf("response event = %q, want message", event)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		t.Fatalf("decode streamed response: %v", err)
	}
	if response["id"] != "stream-list" {
		t.Fatalf("streamed response id = %#v", response["id"])
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("streamed response result missing: %#v", response)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("streamed tools/list returned no tools: %#v", result)
	}
}

func newTestHTTPHandler(t *testing.T) (*HTTPHandler, *bool, *bool, *bool) {
	t.Helper()
	authorizer := rpc.NewMemoryAuthorizer()
	authorizer.AddToken("read.secret", "reader", map[rpc.Capability]rpc.CapabilityGrant{
		rpc.CapabilityRead: {},
	}, time.Now().Add(time.Hour))
	authorizer.AddToken("write.secret", "writer", map[rpc.Capability]rpc.CapabilityGrant{
		rpc.CapabilityWrite: {RepositoryID: "repo_1"},
	}, time.Now().Add(time.Hour))
	authorizer.AddToken("admin.secret", "admin", map[rpc.Capability]rpc.CapabilityGrant{
		rpc.CapabilityAdmin: {RepositoryID: "repo_1"},
	}, time.Now().Add(time.Hour))
	authorizer.AddToken("lane.secret", "lane", map[rpc.Capability]rpc.CapabilityGrant{
		rpc.CapabilityClaim:  {RepositoryID: "repo_1", SessionID: "sess_1"},
		rpc.CapabilityWrite:  {RepositoryID: "repo_1", SessionID: "sess_1"},
		rpc.CapabilityRead:   {RepositoryID: "repo_1", SessionID: "sess_1"},
		rpc.CapabilityReview: {RepositoryID: "repo_1", SessionID: "sess_1"},
	}, time.Now().Add(time.Hour))

	server := rpc.NewServer()
	server.Authorizer = authorizer
	server.Register("status", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		return map[string]any{
			"status":        "ok",
			"repository_id": envelope.Params["repository_id"],
		}, nil
	})
	mutationCalled := false
	server.Register("work.complete", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		mutationCalled = true
		return map[string]any{
			"mutated":       true,
			"repository_id": envelope.Params["repository_id"],
		}, nil
	})
	gitSnapshotCalled := false
	server.Register("git.snapshot", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		gitSnapshotCalled = true
		return map[string]any{
			"schema_version": "striatum.git_snapshot.v1",
			"repository_id":  envelope.Params["repository_id"],
		}, nil
	})
	hiddenCalled := false
	server.Register("workflow.generate", func(context.Context, rpc.Envelope) (map[string]any, error) {
		hiddenCalled = true
		return map[string]any{"hidden": true}, nil
	})
	handler := NewHTTPHandler(Service{RPC: server, Authorizer: authorizer})
	return handler, &mutationCalled, &hiddenCalled, &gitSnapshotCalled
}

func TestHTTPHandlerToolsCallAcceptedRiskRequiresAdmin(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	called := false
	handler.Service.RPC.Register("workflow.accept_risk", func(_ context.Context, envelope rpc.Envelope) (map[string]any, error) {
		called = true
		return map[string]any{"accepted": true, "repository_id": envelope.Params["repository_id"]}, nil
	})
	body := `{"jsonrpc":"2.0","id":"risk-denied","method":"tools/call","params":{"name":"workflow.accept_risk","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "read.secret"))
	structured := structuredContent(t, response)
	if structured["ok"] != false || structured["error"] != "capability_missing" {
		t.Fatalf("read token accepted-risk denial = %#v", structured)
	}
	if called {
		t.Fatal("workflow.accept_risk handler ran for read-only token")
	}

	body = `{"jsonrpc":"2.0","id":"risk-admin","method":"tools/call","params":{"name":"workflow.accept_risk","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`
	response = decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "admin.secret"))
	structured = structuredContent(t, response)
	if structured["ok"] != true || !called {
		t.Fatalf("admin accepted-risk call = %#v called=%v", structured, called)
	}
}

func postJSON(t *testing.T, handler http.Handler, path string, body string, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := newJSONRequest(t, path, body, token)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func newJSONRequest(t *testing.T, path string, body string, token string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Host = "127.0.0.1:8765"
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func decodeTestResponse(t *testing.T, recorder *httptest.ResponseRecorder) jsonrpcResponse {
	t.Helper()
	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json; body = %s", contentType, recorder.Body.String())
	}
	var response jsonrpcResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, recorder.Body.String())
	}
	return response
}

func structuredContent(t *testing.T, response jsonrpcResponse) map[string]any {
	t.Helper()
	if response.Error != nil {
		t.Fatalf("unexpected JSON-RPC error = %#v", response.Error)
	}
	structured, ok := response.Result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("structuredContent missing: %#v", response.Result)
	}
	return structured
}

func assertErrorDataCode(t *testing.T, rpcErr *jsonrpcError, code string) {
	t.Helper()
	if rpcErr == nil {
		t.Fatalf("missing JSON-RPC error, want data code %s", code)
	}
	data, ok := rpcErr.Data.(map[string]any)
	if !ok || data["code"] != code {
		t.Fatalf("error data = %#v, want code %s", rpcErr.Data, code)
	}
}

func toolNames(tools []any) map[string]bool {
	names := map[string]bool{}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if name != "" {
			names[name] = true
		}
	}
	return names
}

func readTestSSEEvent(t *testing.T, reader *bufio.Reader) (string, string) {
	t.Helper()
	event := "message"
	var data []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE event: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(data) > 0 {
				return event, strings.Join(data, "\n")
			}
			event = "message"
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
	}
}

// RFC 0111 P1: the MCP content text block is the channel an LLM agent reads,
// so a failed tools/call must render the error code and message there — not
// the bare method name — while structuredContent keeps the stable contract.
func TestHTTPHandlerToolsCallFailureContentCarriesCodeAndMessage(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.RPC.Register("work.complete", func(context.Context, rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("invalid_transition", "job job_1 is not in a completable state", nil)
	})
	body := `{"jsonrpc":"2.0","id":"failure-content","method":"tools/call","params":{"name":"work.complete","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "write.secret"))
	if response.Result["isError"] != true {
		t.Fatalf("isError = %#v", response.Result["isError"])
	}
	structured := structuredContent(t, response)
	if structured["error"] != "invalid_transition" || structured["error_message"] != "job job_1 is not in a completable state" {
		t.Fatalf("structured error contract changed: %#v", structured)
	}
	text := contentText(t, response)
	if text == "work.complete" {
		t.Fatalf("content text is still the bare method name: %q", text)
	}
	if !strings.Contains(text, "invalid_transition") || !strings.Contains(text, "job job_1 is not in a completable state") {
		t.Fatalf("content text must carry code and message: %q", text)
	}
}

// RFC 0111 P1: when the failure has a code but no message, the content text
// still carries the dispatchable code instead of degrading to the method name.
func TestHTTPHandlerToolsCallFailureContentFallsBackToCode(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.RPC.Register("status", func(context.Context, rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("not_found", "", nil)
	})
	body := `{"jsonrpc":"2.0","id":"failure-code-only","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "read.secret"))
	if response.Result["isError"] != true {
		t.Fatalf("isError = %#v", response.Result["isError"])
	}
	text := contentText(t, response)
	if !strings.Contains(text, "not_found") {
		t.Fatalf("content text must carry the code when the message is empty: %q", text)
	}
}

// RFC 0111 P1: success keeps a terse one-line summary in the content text.
func TestHTTPHandlerToolsCallSuccessContentStaysTerse(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	body := `{"jsonrpc":"2.0","id":"success-content","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "read.secret"))
	if response.Result["isError"] != false {
		t.Fatalf("isError = %#v", response.Result["isError"])
	}
	text := contentText(t, response)
	if !strings.Contains(text, "status") || strings.Contains(text, "failed") {
		t.Fatalf("success content text should be a terse ok summary: %q", text)
	}
}

func contentText(t *testing.T, response jsonrpcResponse) string {
	t.Helper()
	blocks, ok := response.Result["content"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("content blocks missing: %#v", response.Result)
	}
	block, ok := blocks[0].(map[string]any)
	if !ok || block["type"] != "text" {
		t.Fatalf("first content block is not text: %#v", blocks[0])
	}
	text, ok := block["text"].(string)
	if !ok {
		t.Fatalf("content text missing: %#v", block)
	}
	return text
}

// RFC 0111 P2: a failing tools/call carries the remediation end to end — the
// content text appends the suggestion and structuredContent gains a sibling
// suggestion key, while error/error_message semantics stay unchanged. The
// handler sets no explicit suggestion, so this also proves the central
// catalog default-fill reaches the MCP boundary.
func TestHTTPHandlerToolsCallFailureContentCarriesSuggestion(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.RPC.Register("work.complete", func(context.Context, rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("lease_error", "lease is expired", nil)
	})
	body := `{"jsonrpc":"2.0","id":"failure-suggestion","method":"tools/call","params":{"name":"work.complete","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "write.secret"))
	if response.Result["isError"] != true {
		t.Fatalf("isError = %#v", response.Result["isError"])
	}
	structured := structuredContent(t, response)
	if structured["error"] != "lease_error" || structured["error_message"] != "lease is expired" {
		t.Fatalf("structured error contract changed: %#v", structured)
	}
	want := rpc.DefaultSuggestion("lease_error")
	if want == "" {
		t.Fatalf("catalog default for lease_error must be non-empty")
	}
	if structured["suggestion"] != want {
		t.Fatalf("structured suggestion = %#v, want %q", structured["suggestion"], want)
	}
	data, ok := structured["data"].(map[string]any)
	if !ok || data["suggestion"] != want {
		t.Fatalf("RPC Response.Data suggestion missing at the MCP boundary: %#v", structured["data"])
	}
	text := contentText(t, response)
	if !strings.Contains(text, "lease_error") || !strings.Contains(text, "lease is expired") {
		t.Fatalf("content text must keep carrying code and message: %q", text)
	}
	if !strings.Contains(text, "suggestion: "+want) {
		t.Fatalf("content text must carry the suggestion in-band: %q", text)
	}
}

// RFC 0111 P2: an explicit call-site suggestion wins over the catalog default
// all the way through the MCP boundary.
func TestHTTPHandlerToolsCallExplicitSuggestionWinsOverDefault(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.RPC.Register("work.complete", func(context.Context, rpc.Envelope) (map[string]any, error) {
		return nil, &rpc.Error{Code: "lease_error", Message: "lease is expired", Suggestion: "re-claim job job_1 specifically", ExitCode: 10}
	})
	body := `{"jsonrpc":"2.0","id":"explicit-suggestion","method":"tools/call","params":{"name":"work.complete","repository_id":"repo_1","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "write.secret"))
	structured := structuredContent(t, response)
	if structured["suggestion"] != "re-claim job job_1 specifically" {
		t.Fatalf("explicit suggestion lost at MCP boundary: %#v", structured["suggestion"])
	}
	text := contentText(t, response)
	if !strings.Contains(text, "suggestion: re-claim job job_1 specifically") {
		t.Fatalf("content text must carry the explicit suggestion: %q", text)
	}
	if strings.Contains(text, rpc.DefaultSuggestion("lease_error")) {
		t.Fatalf("catalog default must not override the explicit suggestion: %q", text)
	}
}

// RFC 0111 P2: failures whose code has no catalog default keep the P1 shape —
// no dangling suggestion separator, no suggestion key.
func TestHTTPHandlerToolsCallFailureWithoutSuggestionStaysP1Shaped(t *testing.T) {
	handler, _, _, _ := newTestHTTPHandler(t)
	handler.Service.RPC.Register("status", func(context.Context, rpc.Envelope) (map[string]any, error) {
		return nil, rpc.NewError("git_snapshot_failed", "boom", nil)
	})
	body := `{"jsonrpc":"2.0","id":"no-suggestion","method":"tools/call","params":{"name":"status","arguments":{"repository_id":"repo_1"}}}`

	response := decodeTestResponse(t, postJSON(t, handler, EndpointPath, body, "read.secret"))
	structured := structuredContent(t, response)
	if _, exists := structured["suggestion"]; exists {
		t.Fatalf("suggestion key must be absent when there is no remediation: %#v", structured)
	}
	text := contentText(t, response)
	if strings.Contains(text, "suggestion") {
		t.Fatalf("content text must not render an empty suggestion: %q", text)
	}
}
