package rpc

import (
	"context"
	"strings"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	body := []byte(`{"schema_version":1,"request_id":"req_1","method":"daemon.hello","params":{"client":{"supported_envelope":[1],"supported_framings":["json"]}},"deadline_ms":0}`)
	envelope, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	encoded, err := envelope.Encode()
	if err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
	again, err := DecodeEnvelope(encoded)
	if err != nil {
		t.Fatalf("decode encoded envelope: %v", err)
	}
	if again.RequestID != envelope.RequestID || again.Method != envelope.Method {
		t.Fatalf("round trip mismatch: %#v != %#v", again, envelope)
	}
}

func TestEnvelopeAcceptsContractedUndottedMethods(t *testing.T) {
	body := []byte(`{"schema_version":1,"request_id":"req_status","method":"status","params":{},"deadline_ms":0}`)
	envelope, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatalf("decode undotted contract method: %v", err)
	}
	if envelope.Method != "status" {
		t.Fatalf("method = %q, want status", envelope.Method)
	}
}

func TestDescribeRequiresHandshake(t *testing.T) {
	server := NewServer()
	envelope := Envelope{
		SchemaVersion: SupportedEnvelopeVersion,
		RequestID:     "req_1",
		Method:        "daemon.describe",
		Params:        map[string]any{},
	}
	response := server.Handle(context.Background(), envelope, "conn")
	if response.OK {
		t.Fatalf("describe without hello succeeded")
	}
	if response.Data["code"] != "version_incompatible" {
		t.Fatalf("unexpected error: %#v", response.Data)
	}
}

func TestHelloThenDescribe(t *testing.T) {
	server := NewServer()
	hello := Envelope{
		SchemaVersion: SupportedEnvelopeVersion,
		RequestID:     "req_hello",
		Method:        "daemon.hello",
		Params: map[string]any{"client": map[string]any{
			"supported_envelope": []any{float64(1)},
			"supported_framings": []any{"json"},
		}},
	}
	if response := server.Handle(context.Background(), hello, "conn"); !response.OK {
		t.Fatalf("hello failed: %#v", response.Data)
	}
	describe := Envelope{
		SchemaVersion: SupportedEnvelopeVersion,
		RequestID:     "req_describe",
		Method:        "daemon.describe",
		Params:        map[string]any{},
	}
	response := server.Handle(context.Background(), describe, "conn")
	if !response.OK {
		t.Fatalf("describe failed: %#v", response.Data)
	}
	if response.Data["methods_etag"] == "" {
		t.Fatalf("missing methods etag: %#v", response.Data)
	}
}

func TestRetiredDeprecatedRouteReturnsMethodUnknownWithReplacement(t *testing.T) {
	server := NewServer()
	response := server.HandleWithoutHandshake(context.Background(), Envelope{
		SchemaVersion: SupportedEnvelopeVersion,
		RequestID:     "req_retired_recovery_auto",
		Method:        "recovery.auto",
		Params:        map[string]any{"repository_id": "repo_1"},
	}, "mcp")

	if response.OK {
		t.Fatalf("retired recovery.auto unexpectedly succeeded: %#v", response.Data)
	}
	if response.Data["code"] != "method_unknown" {
		t.Fatalf("retired recovery.auto code = %#v, want method_unknown; data=%#v", response.Data["code"], response.Data)
	}
	details, ok := response.Data["details"].(map[string]any)
	if !ok {
		t.Fatalf("retired recovery.auto missing details: %#v", response.Data)
	}
	if details["retired_method"] != "recovery.auto" || details["replacement_method"] != "recovery.sweep" {
		t.Fatalf("retired recovery.auto details = %#v", details)
	}
	if suggestion, _ := response.Data["suggestion"].(string); !strings.Contains(suggestion, "recovery.sweep") {
		t.Fatalf("retired recovery.auto suggestion = %q, want replacement method", suggestion)
	}
}

func TestHelloUsesDynamicSealedApplyStatus(t *testing.T) {
	server := NewServer()
	server.SealedApplyFunc = func() map[string]any {
		return map[string]any{
			"supported":      true,
			"key_loaded":     true,
			"signing_key_id": "ed25519:test",
			"public_key":     "pub",
		}
	}
	hello := Envelope{
		SchemaVersion: SupportedEnvelopeVersion,
		RequestID:     "req_dynamic_hello",
		Method:        "daemon.hello",
		Params: map[string]any{"client": map[string]any{
			"supported_envelope": []any{float64(1)},
			"supported_framings": []any{"json"},
		}},
	}
	response := server.Handle(context.Background(), hello, "conn")
	if !response.OK {
		t.Fatalf("hello failed: %#v", response.Data)
	}
	sealedApply, ok := response.Data["sealed_apply"].(map[string]any)
	if !ok {
		t.Fatalf("sealed_apply missing: %#v", response.Data)
	}
	if sealedApply["signing_key_id"] != "ed25519:test" || sealedApply["public_key"] != "pub" {
		t.Fatalf("dynamic sealed_apply not used: %#v", sealedApply)
	}
}

// RFC 0111 P2: an explicit call-site suggestion rides rpc.Error through
// ErrorResponse into Response.Data verbatim.
func TestErrorResponseCarriesExplicitSuggestion(t *testing.T) {
	err := &Error{Code: "lease_error", Message: "lease is expired", Suggestion: "call-site remediation wins"}
	response := ErrorResponse("req_1", err, "audit_1")
	if response.OK {
		t.Fatalf("error response must not be ok")
	}
	if response.Data["suggestion"] != "call-site remediation wins" {
		t.Fatalf("explicit suggestion lost: %#v", response.Data)
	}
}

// RFC 0111 P2: when a call site sets no suggestion, ErrorResponse centrally
// fills the catalog's per-code default — the design that avoids touching the
// 165+ NewError call sites.
func TestErrorResponseFillsDefaultSuggestionFromCatalog(t *testing.T) {
	response := ErrorResponse("req_1", NewError("lease_error", "lease is expired", nil), "audit_1")
	suggestion, _ := response.Data["suggestion"].(string)
	if suggestion == "" || suggestion != DefaultSuggestion("lease_error") {
		t.Fatalf("default suggestion not filled from catalog: %#v", response.Data)
	}
}

// RFC 0111 P2: codes with no sensible remediation carry no suggestion key at
// all (omitted, not empty).
func TestErrorResponseOmitsSuggestionWhenNoneKnown(t *testing.T) {
	response := ErrorResponse("req_1", NewError("git_snapshot_failed", "boom", nil), "audit_1")
	if _, exists := response.Data["suggestion"]; exists {
		t.Fatalf("suggestion key must be omitted when the catalog has no default: %#v", response.Data)
	}
}

// RFC 0111 P2 acceptance: every high-traffic family code the RFC names (as it
// exists in this codebase) must carry a non-empty default suggestion through
// Response.Data.
func TestHighTrafficCodesCarryNonEmptyDefaultSuggestion(t *testing.T) {
	codes := []string{
		// lifecycle
		"invalid_transition",
		// lease/session
		"lease_error",
		// capability
		"capability_missing", "capability_denied",
		"token_invalid", "token_expired", "token_revoked", "token_malformed",
		// confirmation gates
		"confirmation_required", "branch_confirmation_required",
	}
	for _, code := range codes {
		if DefaultSuggestion(code) == "" {
			t.Errorf("catalog default suggestion missing for high-traffic code %s", code)
			continue
		}
		response := ErrorResponse("req_1", NewError(code, "failure", nil), "audit_1")
		suggestion, _ := response.Data["suggestion"].(string)
		if suggestion == "" {
			t.Errorf("ErrorResponse dropped the default suggestion for %s: %#v", code, response.Data)
		}
	}
}
