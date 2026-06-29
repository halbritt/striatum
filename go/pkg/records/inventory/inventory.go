package inventory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const SchemaVersion = "striatum.records_migration_inventory.v1"

const (
	ClassificationSafeToBlobIndex = "safe_to_blob_index"
	ClassificationKeepInGit       = "keep_in_git"
	ClassificationManualReview    = "manual_review"
)

var defaultRoots = []string{
	"docs/operator",
	"docs/audits",
	"docs/records/_frozen",
	"docs/dogfood",
	"docs/dogfoods",
}

type SourceCommitFunc func(ctx context.Context, repoRoot, relPath string) (string, error)

type Options struct {
	RepoRoot     string
	Roots        []string
	SourceCommit SourceCommitFunc
}

type Manifest struct {
	SchemaVersion string   `json:"schema_version"`
	Roots         []string `json:"roots"`
	Entries       []Entry  `json:"entries"`
}

type Entry struct {
	Path               string `json:"path"`
	Size               int64  `json:"size"`
	SHA256             string `json:"sha256"`
	SourceCommit       string `json:"source_commit"`
	RecordClass        string `json:"record_class"`
	ProposedImportMode string `json:"proposed_import_mode"`
	Classification     string `json:"classification"`
}

func DefaultRoots() []string {
	return append([]string(nil), defaultRoots...)
}

func Run(ctx context.Context, options Options) (Manifest, error) {
	repoRoot := options.RepoRoot
	if strings.TrimSpace(repoRoot) == "" {
		var err error
		repoRoot, err = os.Getwd()
		if err != nil {
			return Manifest{}, err
		}
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return Manifest{}, err
	}
	repoAbs = filepath.Clean(repoAbs)

	roots := options.Roots
	if len(roots) == 0 {
		roots = DefaultRoots()
	}
	resolvedRoots, err := resolveRoots(repoAbs, roots)
	if err != nil {
		return Manifest{}, err
	}

	sourceCommit := options.SourceCommit
	if sourceCommit == nil {
		sourceCommit = GitSourceCommit
	}

	seen := map[string]bool{}
	entries := []Entry{}
	for _, root := range resolvedRoots {
		collected, err := collectRoot(ctx, repoAbs, root.Abs, sourceCommit, seen)
		if err != nil {
			return Manifest{}, err
		}
		entries = append(entries, collected...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })

	rootNames := make([]string, 0, len(resolvedRoots))
	for _, root := range resolvedRoots {
		rootNames = append(rootNames, root.Rel)
	}
	sort.Strings(rootNames)

	return Manifest{
		SchemaVersion: SchemaVersion,
		Roots:         rootNames,
		Entries:       entries,
	}, nil
}

func Classify(relPath string) (recordClass, proposedImportMode, classification string) {
	p := normalizeSlashPath(relPath)
	ext := strings.ToLower(path.Ext(p))
	isMarkdown := ext == ".md"

	switch p {
	case "docs/operator/BRIEF.md", "docs/operator/INDEX.md", "docs/operator/README.md", "docs/operator/rfc-roadmap.md":
		return "operator_current_surface", ClassificationKeepInGit, ClassificationKeepInGit
	case "docs/dogfood/HISTORICAL.md":
		return "dogfood_index", ClassificationKeepInGit, ClassificationKeepInGit
	case "docs/dogfood/FRICTION_LOG.md":
		return "dogfood_friction_log", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	}

	if strings.HasPrefix(p, "docs/dogfoods/") {
		switch path.Base(p) {
		case "workflow.json":
			return "workflow_fixture", ClassificationKeepInGit, ClassificationKeepInGit
		case "README.md":
			return "dogfood_index", ClassificationKeepInGit, ClassificationKeepInGit
		case "OPERATOR_REPORT.md":
			return "operator_report", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
		}
	}

	if !isMarkdown {
		return "unknown", ClassificationManualReview, ClassificationManualReview
	}

	switch {
	case strings.HasPrefix(p, "docs/operator/artifacts/"):
		return "historical_operator_artifact_doc", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/operator/workflows/"):
		return "historical_operator_workflow_doc", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/audits/"):
		return "audit_record", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/records/_frozen/"):
		return "frozen_historical_record", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/operator/briefs/"):
		return "historical_operator_brief", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/operator/plans/"):
		return "operator_work_plan", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/operator/progress/"):
		return "operator_progress_note", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	case strings.HasPrefix(p, "docs/operator/recovery-decisions/"):
		return "operator_decision", ClassificationKeepInGit, ClassificationKeepInGit
	case strings.HasPrefix(p, "docs/operator/retrospectives/"):
		return "operator_retrospective", ClassificationSafeToBlobIndex, ClassificationSafeToBlobIndex
	}

	return "unknown", ClassificationManualReview, ClassificationManualReview
}

func GitSourceCommit(ctx context.Context, repoRoot, relPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "log", "-n", "1", "--format=%H", "--", normalizeSlashPath(relPath))
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

type resolvedRoot struct {
	Abs string
	Rel string
}

func resolveRoots(repoAbs string, roots []string) ([]resolvedRoot, error) {
	result := []resolvedRoot{}
	seen := map[string]bool{}
	for _, root := range roots {
		resolved, err := resolveRoot(repoAbs, root)
		if err != nil {
			return nil, err
		}
		if seen[resolved.Rel] {
			continue
		}
		seen[resolved.Rel] = true
		result = append(result, resolved)
	}
	return result, nil
}

func resolveRoot(repoAbs, root string) (resolvedRoot, error) {
	if strings.TrimSpace(root) == "" {
		return resolvedRoot{}, fmt.Errorf("inventory root cannot be empty")
	}
	cleaned := filepath.Clean(filepath.FromSlash(root))
	if filepath.IsAbs(cleaned) {
		cleaned = filepath.Clean(cleaned)
	} else {
		cleaned = filepath.Join(repoAbs, cleaned)
	}
	rel, err := filepath.Rel(repoAbs, cleaned)
	if err != nil {
		return resolvedRoot{}, err
	}
	if relEscapes(rel) {
		return resolvedRoot{}, fmt.Errorf("inventory root %q escapes repository %q", root, repoAbs)
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return resolvedRoot{}, fmt.Errorf("inventory root %q cannot be read: %w", root, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return resolvedRoot{}, fmt.Errorf("inventory root %q is not a directory or regular file", root)
	}
	return resolvedRoot{Abs: cleaned, Rel: normalizeSlashPath(rel)}, nil
}

func collectRoot(ctx context.Context, repoAbs, rootAbs string, sourceCommit SourceCommitFunc, seen map[string]bool) ([]Entry, error) {
	rootInfo, err := os.Stat(rootAbs)
	if err != nil {
		return nil, err
	}
	if rootInfo.Mode().IsRegular() {
		entry, err := entryForFile(ctx, repoAbs, rootAbs, rootInfo, sourceCommit)
		if err != nil {
			return nil, err
		}
		if seen[entry.Path] {
			return nil, nil
		}
		seen[entry.Path] = true
		return []Entry{entry}, nil
	}

	entries := []Entry{}
	err = filepath.WalkDir(rootAbs, func(filePath string, dirEntry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if dirEntry.IsDir() {
			switch dirEntry.Name() {
			case ".git", ".striatum":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		info, err := dirEntry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		entry, err := entryForFile(ctx, repoAbs, filePath, info, sourceCommit)
		if err != nil {
			return err
		}
		if seen[entry.Path] {
			return nil
		}
		seen[entry.Path] = true
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func entryForFile(ctx context.Context, repoAbs, filePath string, info os.FileInfo, sourceCommit SourceCommitFunc) (Entry, error) {
	rel, err := filepath.Rel(repoAbs, filePath)
	if err != nil {
		return Entry{}, err
	}
	if relEscapes(rel) {
		return Entry{}, fmt.Errorf("inventory path %q escapes repository %q", filePath, repoAbs)
	}
	relPath := normalizeSlashPath(rel)
	sum, err := fileSHA256(filePath)
	if err != nil {
		return Entry{}, err
	}
	source, err := sourceCommit(ctx, repoAbs, relPath)
	if err != nil {
		return Entry{}, err
	}
	recordClass, proposed, classification := Classify(relPath)
	return Entry{
		Path:               relPath,
		Size:               info.Size(),
		SHA256:             sum,
		SourceCommit:       source,
		RecordClass:        recordClass,
		ProposedImportMode: proposed,
		Classification:     classification,
	}, nil
}

func fileSHA256(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func relEscapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func normalizeSlashPath(p string) string {
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(p)))
	if p == "." {
		return "."
	}
	return strings.TrimPrefix(p, "./")
}
