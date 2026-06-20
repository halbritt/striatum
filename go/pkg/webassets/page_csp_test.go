package webassets

import (
	"regexp"
	"strings"
	"testing"
)

// TestRenderPageHasNoCSPBlockedInlineScript is the #358 item-7 regression: the
// dashboard CSP is `script-src 'self'` with no nonce and no unsafe-inline, so an
// inline <script> block or an inline on*= event handler is BLOCKED in a compliant
// browser — which is exactly why the dashboard JS never ran (the load-bearing
// logic was inline; the served app.js was a 53-byte stub). The page must carry no
// inline script: all logic lives in /static/app.js (served under 'self').
func TestRenderPageHasNoCSPBlockedInlineScript(t *testing.T) {
	page, err := RenderPage("Striatum", map[string]any{
		"runs": []map[string]any{{"run_id": "run_abc123", "state": "active", "branch_name": "main"}},
		"sessions": []map[string]any{
			{"session_id": "sess_1", "role_id": "impl", "lane_id": "claude", "state": "active"},
		},
	})
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	html := string(page)

	// The only <script> permitted is the external app.js include (src=, no inline body).
	scriptRe := regexp.MustCompile(`(?s)<script\b([^>]*)>(.*?)</script>`)
	for _, m := range scriptRe.FindAllStringSubmatch(html, -1) {
		attrs, body := m[1], strings.TrimSpace(m[2])
		if !strings.Contains(attrs, `src=`) {
			t.Errorf("page has an inline <script> body (CSP script-src 'self' blocks it); move it into app.js. Body starts: %.80q", body)
		}
		if body != "" {
			t.Errorf("external <script> tag has a non-empty inline body (CSP-blocked): %.80q", body)
		}
	}

	// No inline event handlers (onclick=, onchange=, ...) — CSP also blocks those.
	inlineHandlerRe := regexp.MustCompile(`(?i)\son[a-z]+\s*=\s*["']`)
	if loc := inlineHandlerRe.FindString(html); loc != "" {
		t.Errorf("page has a CSP-blocked inline event handler %q; wire it with addEventListener in app.js", strings.TrimSpace(loc))
	}

	// The template-injected run id must reach app.js via a CSP-safe data- attribute.
	if !strings.Contains(html, `data-selected-run-id="run_abc123"`) {
		t.Errorf("page does not expose the selected run id via a CSP-safe data- attribute; app.js cannot read it")
	}
}

// TestRenderPageSurfacesWorkflowName is the RFC 0042 regression: a selected
// run's card must show the workflow identity (workflow_name), not just the run
// id and branch, so an operator can tell which workflow a run belongs to.
func TestRenderPageSurfacesWorkflowName(t *testing.T) {
	page, err := RenderPage("Striatum", map[string]any{
		"selected_run_id": "run_abc123",
		"runs": []map[string]any{{
			"run_id": "run_abc123", "state": "active", "branch_name": "main",
			"workflow_name": "refactoring-campaign @ v3",
		}},
	})
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	html := string(page)
	if !strings.Contains(html, "Workflow: refactoring-campaign @ v3") {
		t.Errorf("selected run card does not surface the workflow name; got page without 'Workflow: refactoring-campaign @ v3'")
	}
}

// TestRenderPageOmitsWorkflowLineWhenAbsent confirms the workflow line is only
// rendered when a workflow_name is present (a left-join miss leaves it blank,
// not a dangling 'Workflow:' label).
func TestRenderPageOmitsWorkflowLineWhenAbsent(t *testing.T) {
	page, err := RenderPage("Striatum", map[string]any{
		"selected_run_id": "run_abc123",
		"runs": []map[string]any{{
			"run_id": "run_abc123", "state": "active", "branch_name": "main",
		}},
	})
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}
	if strings.Contains(string(page), "Workflow:") {
		t.Errorf("page rendered a 'Workflow:' label with no workflow_name")
	}
}

// TestAppJSCarriesDashboardLogic confirms the load-bearing logic actually moved
// into the served-from-'self' app.js (so it runs under the CSP), rather than the
// page going dark.
func TestAppJSCarriesDashboardLogic(t *testing.T) {
	asset, err := LoadStatic("app.js")
	if err != nil {
		t.Fatalf("LoadStatic(app.js): %v", err)
	}
	body := string(asset.Body)
	for _, needle := range []string{"loadSidebarRuns", "setupDialogueSSE", "selectConsoleStream", "dataset.selectedRunId"} {
		if !strings.Contains(body, needle) {
			t.Errorf("app.js is missing dashboard logic %q (was it left as the 53-byte stub?)", needle)
		}
	}
	if len(body) < 1000 {
		t.Errorf("app.js is only %d bytes; the dashboard logic was not moved in", len(body))
	}
}
