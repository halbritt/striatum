package workflowauthoring

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// claudePrintInvocationPattern matches an actual one-shot `claude --print`/`-p`
// invocation: the `claude` binary followed, ON THE SAME LINE, by a `--print` or
// bare `-p` flag (with optional intervening args). `[^\n]` keeps the match
// line-bounded so a `claude --model …` line cannot spuriously pair with a
// `--print` token many lines later. The guard is about live operational
// invocations (wrappers, workflow JSON lane commands, scripts), and pairs this
// pattern with path-category exclusions for docs that quote the retired pattern
// as prose.
var claudePrintInvocationPattern = regexp.MustCompile(`\bclaude\b[^\n]*?[\s",](--print\b|-p\b)`)

// hygieneExcludedPathPrefixes are path categories that are allowed to contain
// the `claude --print` literal because they are either the enforcement layer
// (lint/refusal code + its tests + workflow fixtures, which carry the string in
// messages, comments, and negative-test fixtures) or historical/retired-status
// documentation that quotes the now-dead pattern as provenance. Chosen by
// path/category, not a brittle per-file list (#199).
var hygieneExcludedPathPrefixes = []string{
	// Enforcement layer: the lint/refusal Go source + tests + generators legitimately
	// carry the literal in error messages, comments, and negative-test fixtures.
	"go/",
	// Historical / retired-status documentation and provenance.
	"docs/records/_frozen/",
	"docs/audits/",
	"docs/rfcs/",
	"docs/decisions/",
	"docs/records/_frozen/research/",
	"docs/operator/workflows/",
	"docs/operator/artifacts/",
	"docs/dogfoods/",
	"prompts/",
}

// hygieneExcludedExactPaths are individual retired-status docs that mention the
// pattern in prose. They are not under a clean prefix, so they are named
// explicitly (the issue enumerates this retired-status category).
var hygieneExcludedExactPaths = map[string]bool{
	"CHANGELOG.md":                                      true,
	"docs/reference/roadmap.md":                         true,
	"docs/reference/spec.md":                            true,
	"docs/reference/ubiquitous-language.md":             true,
	"docs/reference/workflow-types.md":                  true,
	"docs/reference/writing-workflows.md":               true,
	"docs/how-to/writing-workflows.md":                  true,
	"docs/reference/todo.md":                            true,
	"skills/optional/refactoring-campaign/REFERENCE.md": true,
}

func hygienePathExcluded(path string) bool {
	if hygieneExcludedExactPaths[path] {
		return true
	}
	for _, prefix := range hygieneExcludedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// examples/*/README.md: example docs may quote the retired pattern.
	if strings.HasPrefix(path, "examples/") && strings.HasSuffix(path, "/README.md") {
		return true
	}
	return false
}

func repoRootFromTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skipf("no .git found walking up from %s; skipping hygiene guard", wd)
		}
		dir = parent
	}
}

// TestNoTrackedFileInvokesClaudePrint is the #199 CI hygiene guard: walk every
// tracked file and assert none invokes `claude --print`/`-p` outside the
// enforcement layer and historical/retired-status documentation. A new
// operational file (a supervised wrapper, a workflow JSON lane command, a
// script) that re-introduces the retired one-shot mode fails this test. After
// the 2026-06-15 deadline such a live invocation bills API tokens (real money
// per packet) instead of Claude plan usage.
func TestNoTrackedFileInvokesClaudePrint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := repoRootFromTest(t)
	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	var offenders []string
	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" || hygienePathExcluded(rel) {
			continue
		}
		// This guard file itself carries the literal pattern in its regex and
		// comments; it lives under go/ so it is already excluded, but guard
		// defensively in case the exclusion list changes.
		if strings.HasSuffix(rel, "claude_print_hygiene_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			// Submodule / unreadable entry: not an operational invocation.
			continue
		}
		if claudePrintInvocationPattern.Match(content) {
			offenders = append(offenders, rel)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("tracked files invoke the retired `claude --print`/`-p` one-shot mode "+
			"(RFC 0088 / D148; after 2026-06-15 it burns API tokens, real money per packet). "+
			"Use a bare interactive [\"claude\", \"--dangerously-skip-permissions\"] agent-loop lane, "+
			"or if it is retired-status documentation add its path category to the hygiene exclusions:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestClaudePrintHygieneDetectionLogic pins the matcher + exclusion behavior the
// CI guard relies on, so the guard cannot silently pass vacuously: real
// invocations must be detected and prose / interactive lanes must not.
func TestClaudePrintHygieneDetectionLogic(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		match bool
	}{
		{"wrapper invocation", `cat "$f" | claude --print --permission-mode acceptEdits`, true},
		{"short flag invocation", `claude -p "$prompt"`, true},
		{"workflow json lane command", `"command": ["claude", "--print"]`, true}, // a JSON lane command IS a real invocation the guard must catch
		{"interactive lane (supported)", `claude --model opus --dangerously-skip-permissions`, false},
		{"prose mention only", `claude --print is deprecated; use the agent loop`, true},
		{"unrelated -p flag", `grep -p foo bar.txt`, false},
		{"newline does not bridge", "claude --model opus\nsomething --print here", false},
	} {
		if got := claudePrintInvocationPattern.MatchString(tc.body); got != tc.match {
			t.Errorf("%s: MatchString(%q) = %v, want %v", tc.name, tc.body, got, tc.match)
		}
	}

	// Exclusion categories: enforcement code and retired-status docs are skipped;
	// a fresh operational path is not.
	for _, tc := range []struct {
		path     string
		excluded bool
	}{
		{"go/pkg/workflowauthoring/lint.go", true},
		{"docs/records/_frozen/src_templates/bin/claude-supervised-wrapper.sh", true},
		{"docs/rfcs/0049-interactive-claude-lane-mcp-control-plane.md", true},
		{"CHANGELOG.md", true},
		{"docs/audits/STRIATUM_DOCS_DRIFT_AUDIT_GEMINI_3_5_FLASH_2026-06-08.md", true},
		{"docs/reference/roadmap.md", true},
		{"docs/how-to/writing-workflows.md", true},
		{"examples/foo/README.md", true},
		{"skills/optional/refactoring-campaign/REFERENCE.md", true},
		{".striatum/bin/claude-supervised-wrapper.sh", false},
		{"scripts/new-wrapper.sh", false},
		{"some/workflow.json", false},
	} {
		if got := hygienePathExcluded(tc.path); got != tc.excluded {
			t.Errorf("hygienePathExcluded(%q) = %v, want %v", tc.path, got, tc.excluded)
		}
	}
}
