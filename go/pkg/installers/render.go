package installers

import (
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
)

// renderContext is the small set of values consumed by template rendering.
type renderContext struct {
	StriatumVersion string
	Namespace       string
}

// render mirrors src/striatum/skills/install.py:_render — expand the helper
// placeholders first (verbs/boundaries/kinds become Markdown bullet lists),
// then apply str.format_map semantics for {striatum_version} / {namespace}.
func render(raw string, ctx renderContext) (string, error) {
	expanded := expandHelpers(raw)
	return formatMap(expanded, map[string]string{
		"striatum_version": ctx.StriatumVersion,
		"namespace":        ctx.Namespace,
	})
}

// expandHelpers mirrors _expand_helpers: plain string substitution performed
// before format_map so the result only carries {striatum_version}/{namespace}.
func expandHelpers(raw string) string {
	out := raw
	out = strings.ReplaceAll(out, "{verbs_scaffold}", renderVerbs("scaffold"))
	out = strings.ReplaceAll(out, "{verbs_claim_loop}", renderVerbs("claim_loop"))
	out = strings.ReplaceAll(out, "{verbs_supervise}", renderVerbs("supervise"))
	out = strings.ReplaceAll(out, "{verbs_recover}", renderVerbs("recover"))
	out = strings.ReplaceAll(out, "{boundaries_bulletlist}", renderBullets(boundaries))
	out = strings.ReplaceAll(out, "{front_matter_kinds_list}", renderBullets(frontMatterKinds))
	out = strings.ReplaceAll(out, "{front_matter_schema_skeletons}", renderFrontMatterSchemaSkeletons())
	return out
}

func renderVerbs(group string) string {
	rows := verbGroups[group]
	if len(rows) == 0 {
		return "_(no verbs registered)_"
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("- `striatum %s` — %s", row.Verb, row.Summary))
	}
	return strings.Join(lines, "\n")
}

func renderBullets(values []string) string {
	lines := make([]string, 0, len(values))
	for _, value := range values {
		lines = append(lines, "- "+value)
	}
	return strings.Join(lines, "\n")
}

func renderFrontMatterSchemaSkeletons() string {
	lines := make([]string, 0, len(frontMatterSkeletonKinds))
	for _, kind := range frontMatterSkeletonKinds {
		skeleton, ok := artifactcontracts.Skeleton(kind)
		if !ok {
			continue
		}
		lines = append(lines, fmt.Sprintf("### `%s`\n\n```yaml\n%s\n```", kind, skeleton))
	}
	if len(lines) == 0 {
		return "_No registered front-matter schemas._"
	}
	return strings.Join(lines, "\n\n")
}

// formatMap implements the subset of str.format_map the templates rely on:
// {{ and }} are literal braces, {name} substitutes from the mapping (raising on
// an unknown key, like _StrictFormatMap). Format specs/conversions are not used
// by the bundle templates; a name segment before ':' or '!' is honored if one
// ever appears so the grammar stays compatible.
func formatMap(text string, mapping map[string]string) (string, error) {
	var b strings.Builder
	b.Grow(len(text))
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch c {
		case '{':
			if i+1 < len(text) && text[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			end := strings.IndexByte(text[i+1:], '}')
			if end < 0 {
				return "", fmt.Errorf("single '{' encountered in format string")
			}
			field := text[i+1 : i+1+end]
			i += end + 1
			name := field
			if cut := strings.IndexAny(name, ":!"); cut >= 0 {
				name = name[:cut]
			}
			value, ok := mapping[name]
			if !ok {
				return "", fmt.Errorf("template references unknown context key %q", name)
			}
			b.WriteString(value)
		case '}':
			if i+1 < len(text) && text[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			return "", fmt.Errorf("single '}' encountered in format string")
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}
