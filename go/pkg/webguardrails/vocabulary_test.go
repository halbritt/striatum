package webguardrails

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetiredVocabularyGrepGate(t *testing.T) {
	root := repoRoot(t)
	vocabPath := filepath.Join(root, "docs/reference/retired-vocabulary.txt")
	vocabBytes, err := os.ReadFile(vocabPath)
	if err != nil {
		t.Fatalf("failed to read retired vocabulary file: %v", err)
	}

	lines := strings.Split(string(vocabBytes), "\n")
	var tokens []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			tokens = append(tokens, trimmed)
		}
	}

	if len(tokens) == 0 {
		t.Fatal("expected retired-vocabulary.txt to contain at least one token")
	}

	// Paths to search
	pathsToWalk := []string{
		filepath.Join(root, "go"),
		filepath.Join(root, "docs"),
	}

	for _, walkDir := range pathsToWalk {
		err := filepath.WalkDir(walkDir, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				// Skip _archive directories and the relocated frozen records tail
				if entry.Name() == "_archive" || entry.Name() == "_frozen" {
					return filepath.SkipDir
				}
				return nil
			}

			// Exclude the vocabulary list itself
			if path == vocabPath {
				return nil
			}

			// Exclude this test file
			if entry.Name() == "vocabulary_test.go" {
				return nil
			}

			// Determine if this file should be checked
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}

			// Skip files under docs that are historical, decisions, RFCS, or operator runs
			if strings.HasPrefix(rel, "docs/decisions/decision-log.md") ||
				strings.HasPrefix(rel, "docs/reference/ubiquitous-language.md") ||
				strings.HasPrefix(rel, "docs/reference/harness-friction-patterns.md") ||
				strings.HasPrefix(rel, "docs/rfcs/") ||
				strings.HasPrefix(rel, "docs/records/") ||
				strings.HasPrefix(rel, "docs/dogfoods/") ||
				strings.HasPrefix(rel, "docs/operator/") ||
				strings.HasPrefix(rel, "docs/records/_frozen/research/") ||
				strings.HasPrefix(rel, "docs/reference/todo.md") {
				return nil
			}

			// Only check Go files and Markdown files
			ext := filepath.Ext(path)
			if ext != ".go" && ext != ".md" {
				return nil
			}

			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			text := string(body)

			for _, token := range tokens {
				if strings.Contains(text, token) {
					t.Errorf("file %s contains retired vocabulary token %q", rel, token)
				}
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
