package reads

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
)

func artifactPlacementProjection(ctx context.Context, runner db.Runner, alias string) string {
	return ", " + artifactPlacementExpression(ctx, runner, alias) + " AS placement"
}

func artifactPlacementProjectionAny(ctx context.Context, runner any, alias string) string {
	dbRunner, ok := runner.(db.Runner)
	if !ok {
		return ", " + artifactPlacementFallbackExpression(alias+".artifact_kind") + " AS placement"
	}
	return artifactPlacementProjection(ctx, dbRunner, alias)
}

func artifactPlacementExpression(ctx context.Context, runner db.Runner, alias string) string {
	kindExpr := alias + ".artifact_kind"
	fallback := artifactPlacementFallbackExpression(kindExpr)
	if db.ArtifactPlacementColumnPresent(ctx, runner) {
		return fmt.Sprintf("COALESCE(NULLIF(%s.placement, ''), %s)", alias, fallback)
	}
	return fallback
}

func artifactPlacementFallbackExpression(kindExpr string) string {
	kinds := []string{}
	for kind := range artifactcontracts.LegacyBlobExhaustKindSet() {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	quoted := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		quoted = append(quoted, "'"+strings.ReplaceAll(kind, "'", "''")+"'")
	}
	return fmt.Sprintf(
		"CASE WHEN %s IN (%s) THEN '%s' ELSE '%s' END",
		kindExpr,
		strings.Join(quoted, ", "),
		artifactcontracts.PlacementBlobExhaust,
		artifactcontracts.PlacementGitPublication,
	)
}

func decorateArtifactPlacements(rows []map[string]any) {
	for _, row := range rows {
		if _, ok := row["placement"].(string); ok && artifactcontracts.IsAllowedPlacement(fmt.Sprint(row["placement"])) {
			continue
		}
		kind := stringFrom(row, "artifact_kind")
		if kind == "" {
			kind = stringFrom(row, "kind")
		}
		row["placement"] = artifactcontracts.ResolvePlacement(kind, row["placement"])
	}
}
