package reads

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactionTierValidation(t *testing.T) {
	for _, tier := range []string{"public", "curated", "internal"} {
		got, err := redactionTierFromEnvelope(map[string]any{"redaction_tier": tier})
		if err != nil {
			t.Fatalf("%s rejected: %v", tier, err)
		}
		if got != tier {
			t.Fatalf("tier = %q, want %q", got, tier)
		}
	}
	if got, err := redactionTierFromEnvelope(map[string]any{}); err != nil || got != "public" {
		t.Fatalf("default tier = %q, err = %v", got, err)
	}
	if _, err := redactionTierFromEnvelope(map[string]any{"redaction_tier": "private"}); err == nil {
		t.Fatal("expected invalid tier to be rejected")
	}
}

func TestCorpusSourcePathValidation(t *testing.T) {
	denied := []string{
		".env",
		".env.local",
		"keys/private.pem",
		".striatum/retired-local-state",
		"transcripts/session.md",
		"Raw_Model_Output/out.log",
		"Terminal_Output/session.log",
		"docs/transcript.txt",
	}
	for _, path := range denied {
		t.Run(path, func(t *testing.T) {
			if err := validateCorpusSourcePath(path); err == nil {
				t.Fatal("expected path to be denied")
			}
		})
	}
	if err := validateCorpusSourcePath("docs/rfcs/0044-engram-phase-1-implementation-spec.md"); err != nil {
		t.Fatalf("curated markdown path rejected: %v", err)
	}
}

func TestEventAndCommitRedaction(t *testing.T) {
	event := redactEventPayload(map[string]any{
		"event_id":     "evt_1",
		"event_type":   "job.completed",
		"description":  "secret",
		"payload_json": map[string]any{"body": "secret"},
		"row_hash":     "hash",
	})
	if len(event) != 3 || event["description"] != nil || event["payload_json"] != nil {
		t.Fatalf("event redaction = %#v", event)
	}
	token := strings.Repeat("a", 64)
	redacted := redactCommitMessage("Subject\n\nCo-Authored-By: Person <p@example.com>\n" + token + "\nnormal " + token)
	if strings.Contains(redacted, "example.com") || strings.Contains(redacted, "\n"+token+"\n") {
		t.Fatalf("commit redaction leaked sensitive data: %q", redacted)
	}
	if !strings.Contains(redacted, "normal <redacted-token>") {
		t.Fatalf("commit token replacement missing: %q", redacted)
	}
}

func TestGeneratedCorpusTextRedactsTokenAndControls(t *testing.T) {
	token := strings.Repeat("a", 64)
	got := redactGeneratedCorpusText("\x1b[31msecret " + token + "\r\nnext\t  ")
	if strings.Contains(got, token) || strings.Contains(got, "\x1b") || strings.Contains(got, "\r") {
		t.Fatalf("generated corpus redaction leaked raw material: %q", got)
	}
	if !strings.Contains(got, "<redacted-token>") || !strings.Contains(got, "next") {
		t.Fatalf("generated corpus redaction missing expected text: %q", got)
	}
}

func TestEvidenceRedactionDefaultDenyAndSafeScalars(t *testing.T) {
	synthetic := "SYNTHETIC_INJECTION exfiltrate terminal transcripts"
	redacted := redactEvidencePayload(map[string]any{
		"jobs": []any{
			map[string]any{
				"job_id":          "job_1",
				"workflow_job_id": "draft",
				"display_model":   map[string]any{"prompt": synthetic},
				"title":           synthetic,
				"dependencies": []any{
					map[string]any{
						"depends_on_job_id": "job_0",
						"required_verdicts": []any{"accept", map[string]any{"prompt": synthetic}},
						"latest_verdict":    map[string]any{"rationale": synthetic},
					},
				},
			},
		},
		"verdicts": []any{map[string]any{"verdict_id": "verdict_1", "rationale": synthetic, "transcript_excerpt": synthetic}},
		"blockers": []any{map[string]any{"blocker_id": "blocker_1", "description": synthetic, "terminal_output": synthetic}},
		"future":   synthetic,
	})
	body, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("marshal redacted: %v", err)
	}
	if strings.Contains(string(body), synthetic) {
		t.Fatalf("redaction leaked synthetic text: %s", body)
	}
	jobs := redacted["jobs"].([]any)
	job := jobs[0].(map[string]any)
	if job["display_model"] != evidenceFreeTextPlaceholder || job["title"] != evidenceFreeTextPlaceholder {
		t.Fatalf("job redaction = %#v", job)
	}
	if redacted["future"] != evidenceFreeTextPlaceholder {
		t.Fatalf("unknown field = %#v", redacted["future"])
	}
}

func TestRunSummaryAndArchiveRedactProseFields(t *testing.T) {
	summary := redactRunSummaryPayload(map[string]any{
		"status":         map[string]any{"runs": []any{map[string]any{"run_id": "run_1", "secret": "x"}}},
		"doctor":         map[string]any{"ok": false, "availability_ok": false, "provenance_ok": true, "problems": []any{}, "secret": "x"},
		"blockers":       []any{map[string]any{"blocker_id": "blk", "description": "secret", "state": "open"}},
		"branch_context": map[string]any{},
		"timing":         map[string]any{"completed_at": nil, "duration": "0h 0m 7s"},
		"artifacts":      []any{},
		"sessions":       []any{},
		"verdicts":       []any{},
		"future":         "secret",
	})
	if summary["future"] != evidenceFreeTextPlaceholder {
		t.Fatalf("future field = %#v", summary["future"])
	}
	doctor := summary["doctor"].(map[string]any)
	if doctor["ok"] != false || doctor["availability_ok"] != false || doctor["provenance_ok"] != true {
		t.Fatalf("doctor redaction dropped health planes: %#v", doctor)
	}
	if doctor["secret"] != evidenceFreeTextPlaceholder {
		t.Fatalf("doctor redaction leaked unknown field: %#v", doctor)
	}
	timing := summary["timing"].(map[string]any)
	if timing["duration"] != nil {
		t.Fatalf("open-run duration leaked: %#v", timing)
	}

	session := redactArchiveRow("sessions", map[string]any{"session_id": "sess_1", "close_reason": "private"})
	if session["close_reason"] != evidenceFreeTextPlaceholder {
		t.Fatalf("session archive row = %#v", session)
	}
	verdict := redactArchiveRow("verdicts", map[string]any{"verdict_id": "verdict_1", "rationale": "private"})
	if verdict["rationale"] != evidenceFreeTextPlaceholder {
		t.Fatalf("verdict archive row = %#v", verdict)
	}
	event := redactArchiveRow("events", map[string]any{
		"event_id":     "evt_1",
		"payload_json": map[string]any{"event_id": "evt_1", "description": "private", "row_hash": "hash"},
	})
	payload := event["payload_json"].(map[string]any)
	if payload["description"] != nil || payload["row_hash"] != "hash" {
		t.Fatalf("event archive row = %#v", event)
	}
}
