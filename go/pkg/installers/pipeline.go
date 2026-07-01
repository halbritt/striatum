package installers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// planEntry is one rendered file the installer intends to place.
type planEntry struct {
	Path           string // relative to the pipeline base directory
	Rendered       string
	Template       string
	TemplateSHA256 string
}

// fileResult is one per-file result returned by installer planning/apply.
type fileResult struct {
	Path         string  `json:"path"`
	Status       string  `json:"status"`
	SHA256Before *string `json:"sha256_before"`
	SHA256After  string  `json:"sha256_after"`
}

// manifestFile is one row in a written manifest.
type manifestFile struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	Template       string `json:"template"`
	TemplateSHA256 string `json:"template_sha256"`
}

// manifestDoc is the on-disk manifest. Field order is stable for compatibility
// with existing generated manifests.
type manifestDoc struct {
	SchemaVersion   string         `json:"schema_version"`
	StriatumVersion string         `json:"striatum_version"`
	GeneratedAt     string         `json:"generated_at"`
	Profile         string         `json:"profile"`
	Namespace       string         `json:"namespace"`
	Scope           string         `json:"scope"`
	Files           []manifestFile `json:"files"`
}

// pipelineParams configure a single-profile install run.
type pipelineParams struct {
	BaseDir       string // directory plan paths resolve against
	ManifestPath  string // absolute manifest path
	SchemaVersion string
	Version       string
	Profile       string
	Namespace     string
	Scope         string
	Plan          []planEntry
	Force         bool
	DryRun        bool
}

// runPipeline applies the shared edit-detection + manifest logic ported from
// src/striatum/skills/install.py and src/striatum/plugins/install.py.
func runPipeline(p pipelineParams) ([]fileResult, error) {
	existing := map[string]manifestFile{}
	if doc := loadManifest(p.ManifestPath, p.SchemaVersion); doc != nil {
		for _, entry := range doc.Files {
			existing[entry.Path] = entry
		}
	}

	fileResults := make([]fileResult, 0, len(p.Plan))
	nextFiles := make([]manifestFile, 0, len(p.Plan))

	for _, entry := range p.Plan {
		renderedSHA := sha256Hex([]byte(entry.Rendered))
		absolute := filepath.Join(p.BaseDir, filepath.FromSlash(entry.Path))
		manifestEntry, hasManifestEntry := existing[entry.Path]

		var onDisk []byte
		if data, err := os.ReadFile(absolute); err == nil {
			onDisk = data
		}
		var onDiskSHA *string
		if onDisk != nil {
			s := sha256Hex(onDisk)
			onDiskSHA = &s
		}

		decision := decide(onDiskSHA, renderedSHA, hasManifestEntry, manifestEntry, p.Force)
		if p.DryRun && decision == "written" {
			decision = "dry_run"
		}

		if decision == "written" {
			if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(absolute, []byte(entry.Rendered), 0o644); err != nil {
				return nil, err
			}
		}

		switch {
		case decision == "written" || decision == "skipped_unchanged":
			sha := renderedSHA
			if decision == "skipped_unchanged" {
				if onDiskSHA != nil {
					sha = *onDiskSHA
				}
			}
			nextFiles = append(nextFiles, manifestFile{
				Path:           entry.Path,
				SHA256:         sha,
				Template:       entry.Template,
				TemplateSHA256: entry.TemplateSHA256,
			})
		case decision == "refused_modified" && hasManifestEntry:
			sha := manifestEntry.SHA256
			if sha == "" {
				sha = renderedSHA
			}
			tmplSHA := manifestEntry.TemplateSHA256
			if tmplSHA == "" {
				tmplSHA = entry.TemplateSHA256
			}
			nextFiles = append(nextFiles, manifestFile{
				Path:           entry.Path,
				SHA256:         sha,
				Template:       entry.Template,
				TemplateSHA256: tmplSHA,
			})
		case decision == "dry_run" && hasManifestEntry:
			nextFiles = append(nextFiles, manifestEntry)
		}

		fileResults = append(fileResults, fileResult{
			Path:         entry.Path,
			Status:       decision,
			SHA256Before: onDiskSHA,
			SHA256After:  renderedSHA,
		})
	}

	sort.Slice(nextFiles, func(i, j int) bool { return nextFiles[i].Path < nextFiles[j].Path })

	if !p.DryRun {
		doc := manifestDoc{
			SchemaVersion:   p.SchemaVersion,
			StriatumVersion: p.Version,
			GeneratedAt:     utcNow(),
			Profile:         p.Profile,
			Namespace:       p.Namespace,
			Scope:           p.Scope,
			Files:           nextFiles,
		}
		if err := writeManifest(p.ManifestPath, doc); err != nil {
			return nil, err
		}
	}

	return fileResults, nil
}

func decide(onDiskSHA *string, renderedSHA string, hasManifestEntry bool, manifestEntry manifestFile, force bool) string {
	if onDiskSHA == nil {
		return "written"
	}
	if *onDiskSHA == renderedSHA {
		return "skipped_unchanged"
	}
	if !hasManifestEntry {
		return "written"
	}
	if *onDiskSHA == manifestEntry.SHA256 {
		return "written"
	}
	if force {
		return "written"
	}
	return "refused_modified"
}

func loadManifest(path, schemaVersion string) *manifestDoc {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc manifestDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if doc.SchemaVersion != schemaVersion {
		return nil
	}
	return &doc
}

func writeManifest(path string, doc manifestDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o644)
}

func utcNow() string {
	return time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}
