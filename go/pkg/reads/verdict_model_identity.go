package reads

import (
	"context"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
)

const verdictModelIdentityUnknown = "unknown"

func verdictModelIdentityProjection(ctx context.Context, runner db.Runner, alias string) string {
	if !verdictModelIdentityColumnsPresent(ctx, runner) {
		return `,
		       'unknown' AS model_identity_declared,
		       'unknown' AS model_family_at_record,
		       'unknown' AS model_identity_basis,
		       'unknown' AS model_co_blindness_at_record`
	}
	prefix := ""
	if strings.TrimSpace(alias) != "" {
		prefix = alias + "."
	}
	return `,
		       COALESCE(NULLIF(` + prefix + `model_identity_declared, ''), 'unknown') AS model_identity_declared,
		       COALESCE(NULLIF(` + prefix + `model_family_at_record, ''), 'unknown') AS model_family_at_record,
		       COALESCE(NULLIF(` + prefix + `model_identity_basis, ''), 'unknown') AS model_identity_basis,
		       COALESCE(NULLIF(` + prefix + `model_co_blindness_at_record, ''), 'unknown') AS model_co_blindness_at_record`
}

func verdictModelIdentityColumnsPresent(ctx context.Context, runner db.Runner) bool {
	value, err := runner.QueryScalar(ctx, `
		SELECT (
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'verdicts'
		             AND column_name = 'model_identity_declared')
		  AND
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'verdicts'
		             AND column_name = 'model_family_at_record')
		  AND
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'verdicts'
		             AND column_name = 'model_identity_basis')
		  AND
		  EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_schema = 'striatumd' AND table_name = 'verdicts'
		             AND column_name = 'model_co_blindness_at_record')
		)::text`)
	if err != nil {
		return false
	}
	return value == "true" || value == "t"
}

func decorateVerdictModelIdentities(rows []map[string]any) {
	for _, row := range rows {
		decorateVerdictModelIdentity(row)
	}
}

func decorateVerdictModelIdentity(row map[string]any) {
	for _, key := range []string{
		"model_identity_declared",
		"model_family_at_record",
		"model_identity_basis",
		"model_co_blindness_at_record",
	} {
		if text, ok := row[key].(string); !ok || strings.TrimSpace(text) == "" {
			row[key] = verdictModelIdentityUnknown
		}
	}
}
