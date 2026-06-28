package migration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ImportSchemaVersion       = "striatum.records_migration_import.v1"
	MaterializeSchemaVersion  = "striatum.records_migration_materialize.v1"
	VerificationSchemaVersion = "striatum.records_migration_verification.v1"

	DefaultRetentionClass = "historical_generated_record"
)

type ManifestEntry struct {
	Path               string `json:"path"`
	Size               int64  `json:"size"`
	SHA256             string `json:"sha256"`
	SourceCommit       string `json:"source_commit"`
	RecordClass        string `json:"record_class"`
	ProposedImportMode string `json:"proposed_import_mode,omitempty"`
	Classification     string `json:"classification,omitempty"`
}

type CompareProblem struct {
	Code   string `json:"code"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail,omitempty"`
}

func RecordID(sourceCommit, sourcePath, contentSHA256 string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(sourceCommit),
		NormalizePath(sourcePath),
		strings.ToLower(strings.TrimSpace(contentSHA256)),
	}, "\n")))
	return "rec_" + hex.EncodeToString(sum[:])[:32]
}

func BlobKey(sourceCommit, sourcePath, contentSHA256 string) string {
	commit := sanitizeKeySegment(sourceCommit)
	if commit == "" {
		commit = "working-tree"
	}
	if len(commit) > 16 {
		commit = commit[:16]
	}
	digest := strings.ToLower(strings.TrimSpace(contentSHA256))
	if len(digest) > 16 {
		digest = digest[:16]
	}
	return "records/historical/" + commit + "/" + digest + "/" + NormalizePath(sourcePath)
}

func NormalizePath(p string) string {
	p = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(p))))
	if p == "." {
		return ""
	}
	return strings.TrimPrefix(p, "./")
}

func PathEscapesRepository(p string) bool {
	normalized := NormalizePath(p)
	return normalized == "" || normalized == ".." || strings.HasPrefix(normalized, "../") || filepath.IsAbs(normalized)
}

func CompareManifests(expected []ManifestEntry, reconstructed []ManifestEntry) []CompareProblem {
	left := map[string]ManifestEntry{}
	right := map[string]ManifestEntry{}
	problems := []CompareProblem{}
	for _, entry := range expected {
		entry.Path = NormalizePath(entry.Path)
		if _, exists := left[entry.Path]; exists {
			problems = append(problems, CompareProblem{Code: "duplicate_original_manifest_path", Path: entry.Path})
			continue
		}
		left[entry.Path] = entry
	}
	for _, entry := range reconstructed {
		entry.Path = NormalizePath(entry.Path)
		if _, exists := right[entry.Path]; exists {
			problems = append(problems, CompareProblem{Code: "duplicate_reconstructed_manifest_path", Path: entry.Path})
			continue
		}
		right[entry.Path] = entry
	}
	paths := make([]string, 0, len(left)+len(right))
	seen := map[string]bool{}
	for path := range left {
		paths = append(paths, path)
		seen[path] = true
	}
	for path := range right {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		l, lok := left[path]
		r, rok := right[path]
		switch {
		case !lok:
			problems = append(problems, CompareProblem{Code: "unexpected_reconstructed_record", Path: path})
		case !rok:
			problems = append(problems, CompareProblem{Code: "missing_reconstructed_record", Path: path})
		case !strings.EqualFold(l.SHA256, r.SHA256):
			problems = append(problems, CompareProblem{Code: "reconstructed_sha256_mismatch", Path: path, Detail: fmt.Sprintf("expected=%s got=%s", strings.ToLower(l.SHA256), strings.ToLower(r.SHA256))})
		case l.Size != r.Size:
			problems = append(problems, CompareProblem{Code: "reconstructed_size_mismatch", Path: path, Detail: fmt.Sprintf("expected=%d got=%d", l.Size, r.Size)})
		}
	}
	return problems
}

func sanitizeKeySegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	previousDash := false
	for _, r := range value {
		valid := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
		if !valid {
			r = '-'
		}
		if r == '-' {
			if previousDash || b.Len() == 0 {
				continue
			}
			previousDash = true
		} else {
			previousDash = false
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "-")
}
