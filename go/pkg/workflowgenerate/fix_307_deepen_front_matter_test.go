package workflowgenerate

import (
	"strings"
	"testing"
)

// #307: the deepener role stub and the deepen prompt stub must instruct the
// deepener to emit a uniform synthesis.v1 front matter across every model lane —
// an `author:` byline and a COMPLETE `inputs:` list naming both the convergence
// ledger (CONVERGENCE.md) and the problem brief (PROBLEM_BRIEF.md). Before this
// fix one lane (gemini) omitted `author:` from front matter and listed only the
// convergence ledger, so the two deepen artifacts were non-uniform.
func TestDeepenStubsRequireUniformFrontMatter(t *testing.T) {
	role := roleStub("deepener")
	prompt := promptStub("deepen.md")

	for _, tc := range []struct {
		name string
		text string
	}{
		{"deepener role stub", role},
		{"deepen prompt stub", prompt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lower := strings.ToLower(tc.text)
			if !strings.Contains(lower, "author:") {
				t.Errorf("%s must instruct the deepener to emit an author: front-matter line; got:\n%s", tc.name, tc.text)
			}
			if !strings.Contains(lower, "inputs:") {
				t.Errorf("%s must instruct the deepener to emit a complete inputs: list; got:\n%s", tc.name, tc.text)
			}
			if !strings.Contains(tc.text, "CONVERGENCE.md") {
				t.Errorf("%s must name the convergence ledger (CONVERGENCE.md) in inputs:; got:\n%s", tc.name, tc.text)
			}
			if !strings.Contains(tc.text, "PROBLEM_BRIEF.md") {
				t.Errorf("%s must name the problem brief (PROBLEM_BRIEF.md) in inputs:; got:\n%s", tc.name, tc.text)
			}
		})
	}

	// The deepen artifact kind is synthesis (see shapes_divergent.go), so the
	// stubs must reference the synthesis schema the agent has to satisfy.
	if !strings.Contains(role, "synthesis") {
		t.Errorf("deepener role stub should reference the synthesis artifact shape; got:\n%s", role)
	}

	// The stubs must actually reach the rendered role/prompt files for a
	// divergent_ideation scaffold, so the deepener consumes them at runtime.
	gen := mustGenerate(t, divergentRaw("local", nil, nil, nil))
	roleFile := generatedFileContent(t, gen, "docs/operator/workflows/di-test/roles/deepener.md")
	if !strings.Contains(strings.ToLower(roleFile), "author:") || !strings.Contains(roleFile, "PROBLEM_BRIEF.md") {
		t.Errorf("rendered deepener.md role file missing uniform front-matter instruction; got:\n%s", roleFile)
	}
	promptFile := generatedFileContent(t, gen, "docs/operator/workflows/di-test/prompts/deepen.md")
	if !strings.Contains(strings.ToLower(promptFile), "author:") || !strings.Contains(promptFile, "CONVERGENCE.md") {
		t.Errorf("rendered deepen.md prompt file missing uniform front-matter instruction; got:\n%s", promptFile)
	}
}

func generatedFileContent(t *testing.T, gen Generated, path string) string {
	t.Helper()
	for _, f := range gen.Files {
		if f["path"] == path {
			return f["content"].(string)
		}
	}
	paths := make([]string, 0, len(gen.Files))
	for _, f := range gen.Files {
		paths = append(paths, f["path"].(string))
	}
	t.Fatalf("generated files missing %q (have %v)", path, paths)
	return ""
}
