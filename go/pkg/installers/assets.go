// Package installers renders the RFC 0015 skill bundle and RFC 0025 plugin
// bundle install pipelines from embedded Go assets. The rendered bundles keep
// the curated context tables from the retired installer path: verbs,
// boundaries, and artifact kinds.
package installers

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
)

// embedded carries the byte-equivalent copies of the template trees that
// previously lived under src/striatum/skills/templates and
// src/striatum/plugins/templates. Retired package-data markers are
// intentionally not embedded (RFC 0078 Gate B drops them).
//
//go:embed templates/skills templates/plugins templates/optional
var embedded embed.FS

// readOptionalSkillFile returns the raw bytes of one file within a
// Striatum-authored optional skill bundle (#177), rooted at
// templates/optional. Unlike the core skill templates these are copied
// verbatim — they are authored bundles, not parameterized templates, and carry
// literal braces (shell ${...}) that the format_map renderer would reject.
func readOptionalSkillFile(relpath string) ([]byte, error) {
	body, err := embedded.ReadFile("templates/optional/" + relpath)
	if err != nil {
		if errFsNotExist(err) {
			return nil, fmt.Errorf("optional skill file not found: %s", relpath)
		}
		return nil, err
	}
	return body, nil
}

// optionalSkillFiles lists the embedded files under one optional skill bundle,
// as slash-relative paths beneath the skill directory (e.g. "SKILL.md",
// "scripts/instantiate.sh").
func optionalSkillFiles(name string) ([]string, error) {
	root := "templates/optional/" + name
	var files []string
	err := fs.WalkDir(embedded, root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := path[len(root)+1:]
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// optionalSkillNames lists the embedded Striatum-authored optional skill
// bundle names (the immediate child directories of templates/optional).
func optionalSkillNames() ([]string, error) {
	entries, err := fs.ReadDir(embedded, "templates/optional")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// readSkillTemplate returns the raw bytes of a skills template, rooted at
// the former striatum.skills.templates package (e.g. "claude_code/workflow.md.tmpl").
func readSkillTemplate(relpath string) (string, error) {
	body, err := embedded.ReadFile("templates/skills/" + relpath)
	if err != nil {
		if errFsNotExist(err) {
			return "", fmt.Errorf("skills template not found: %s", relpath)
		}
		return "", err
	}
	return string(body), nil
}

// readPluginTemplate returns the raw bytes of a plugin template, rooted at
// the former striatum.plugins.templates package.
func readPluginTemplate(relpath string) (string, error) {
	body, err := embedded.ReadFile("templates/plugins/" + relpath)
	if err != nil {
		if errFsNotExist(err) {
			return "", fmt.Errorf("plugin template not found: %s", relpath)
		}
		return "", err
	}
	return string(body), nil
}

func errFsNotExist(err error) bool {
	return err == fs.ErrNotExist || (err != nil && err.Error() == "file does not exist")
}

func sha256Hex(data []byte) string {
	if data == nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
