package mutations

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const (
	verdictModelUnknown              = "unknown"
	verdictModelBasisWorkflowDisplay = "workflow_snapshot.display_model"
	verdictModelBasisWorkflowModel   = "workflow_snapshot.model"

	verdictCoBlindnessUnknown     = "unknown"
	verdictCoBlindnessIndependent = "independent_model_family"
	verdictCoBlindnessSameFamily  = "same_model_family"
	verdictCoBlindnessNotReview   = "not_review_pair"
)

var verdictModelTokenPattern = regexp.MustCompile(`[^a-z0-9]+`)

type verdictModelIdentityStamp struct {
	Declared    string
	Family      string
	Basis       string
	CoBlindness string
}

type verdictRowInsert struct {
	RepositoryID                string
	VerdictID                   string
	RunID                       any
	JobID                       string
	SessionID                   any
	Verdict                     string
	Rationale                   any
	FindingsArtifactID          any
	CreatedAt                   any
	Posture                     string
	LaneAttestationAtRecord     any
	ReviewProvenanceOverride    bool
	ReviewProvenanceDecisionID  any
	SupervisorIDAtRecord        any
	ReviewGeneration            any
	IncludeReviewGeneration     bool
	ModelIdentity               verdictModelIdentityStamp
	IncludeModelIdentityColumns bool
}

func insertVerdictRow(ctx context.Context, runner any, row verdictRowInsert) error {
	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}
	columns := []string{
		"repository_id", "verdict_id", "run_id", "job_id", "session_id", "verdict",
		"rationale", "findings_artifact_id", "created_at", "posture",
		"lane_attestation_at_record", "review_provenance_override",
		"review_provenance_decision_id", "supervisor_id_at_record",
	}
	args := []any{
		row.RepositoryID, row.VerdictID, row.RunID, row.JobID, row.SessionID, row.Verdict,
		row.Rationale, row.FindingsArtifactID, row.CreatedAt, row.Posture,
		row.LaneAttestationAtRecord, row.ReviewProvenanceOverride,
		row.ReviewProvenanceDecisionID, row.SupervisorIDAtRecord,
	}
	if row.IncludeReviewGeneration {
		columns = append(columns, "review_generation")
		args = append(args, row.ReviewGeneration)
	}
	if row.IncludeModelIdentityColumns {
		stamp := normalizeVerdictModelIdentityStamp(row.ModelIdentity)
		columns = append(columns,
			"model_identity_declared",
			"model_family_at_record",
			"model_identity_basis",
			"model_co_blindness_at_record",
		)
		args = append(args, stamp.Declared, stamp.Family, stamp.Basis, stamp.CoBlindness)
	}
	placeholders := make([]string, 0, len(args))
	for index := range args {
		placeholders = append(placeholders, fmt.Sprintf("$%d", index+1))
	}
	return exec.Exec(ctx, `
		INSERT INTO striatumd.verdicts (`+strings.Join(columns, ", ")+`)
		VALUES (`+strings.Join(placeholders, ", ")+`)`, args...)
}

func normalizeVerdictModelIdentityStamp(stamp verdictModelIdentityStamp) verdictModelIdentityStamp {
	if strings.TrimSpace(stamp.Declared) == "" {
		stamp.Declared = verdictModelUnknown
	}
	if strings.TrimSpace(stamp.Family) == "" {
		stamp.Family = verdictModelUnknown
	}
	if strings.TrimSpace(stamp.Basis) == "" {
		stamp.Basis = verdictModelUnknown
	}
	if strings.TrimSpace(stamp.CoBlindness) == "" {
		stamp.CoBlindness = verdictCoBlindnessUnknown
	}
	return stamp
}

func verdictModelIdentityColumnsPresent(ctx context.Context, runner any) bool {
	row, err := oneRow(ctx, runner, `
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
		) AS present`)
	return err == nil && boolValue(row["present"])
}

func verdictModelIdentitySelectProjection(ctx context.Context, runner any, alias string) string {
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

func verdictModelIdentityStampFromRow(row map[string]any) verdictModelIdentityStamp {
	return normalizeVerdictModelIdentityStamp(verdictModelIdentityStamp{
		Declared:    verdictModelStringFromAny(nullable(row["model_identity_declared"])),
		Family:      verdictModelStringFromAny(nullable(row["model_family_at_record"])),
		Basis:       verdictModelStringFromAny(nullable(row["model_identity_basis"])),
		CoBlindness: verdictModelStringFromAny(nullable(row["model_co_blindness_at_record"])),
	})
}

func attachVerdictModelIdentityFields(target map[string]any, row map[string]any) {
	stamp := verdictModelIdentityStampFromRow(row)
	target["model_identity_declared"] = stamp.Declared
	target["model_family_at_record"] = stamp.Family
	target["model_identity_basis"] = stamp.Basis
	target["model_co_blindness_at_record"] = stamp.CoBlindness
}

func verdictModelIdentityForJob(ctx context.Context, runner any, repositoryID string, job map[string]any, overrideBasis string) verdictModelIdentityStamp {
	if strings.TrimSpace(overrideBasis) != "" {
		return verdictModelIdentityUnknown(overrideBasis)
	}
	workflow := workflowSnapshotForVerdictModelIdentity(ctx, runner, repositoryID, job)
	if len(workflow) == 0 {
		return verdictModelIdentityUnknown(verdictModelUnknown)
	}
	stamp := verdictModelIdentityForWorkflowJob(workflow, job)
	stamp.CoBlindness = verdictModelCoBlindness(ctx, runner, repositoryID, workflow, job, stamp.Family)
	return normalizeVerdictModelIdentityStamp(stamp)
}

func verdictModelIdentityUnknown(basis string) verdictModelIdentityStamp {
	if strings.TrimSpace(basis) == "" {
		basis = verdictModelUnknown
	}
	return verdictModelIdentityStamp{
		Declared:    verdictModelUnknown,
		Family:      verdictModelUnknown,
		Basis:       basis,
		CoBlindness: verdictCoBlindnessUnknown,
	}
}

func workflowSnapshotForVerdictModelIdentity(ctx context.Context, runner any, repositoryID string, job map[string]any) map[string]any {
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return map[string]any{}
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(run["workflow_snapshot_id"]), false)
	if err != nil {
		return map[string]any{}
	}
	return asMap(snapshot["workflow_json"])
}

func verdictModelIdentityForWorkflowJob(workflow map[string]any, job map[string]any) verdictModelIdentityStamp {
	def := workflowJobDefinitionFromWorkflow(workflow, fmt.Sprint(job["workflow_job_id"]))
	laneID := verdictIdentityLaneID(def, job)
	lanes := asMap(workflow["lanes"])
	lane := asMap(lanes[laneID])
	if declared := declaredWorkflowString(lane["display_model"]); declared != "" {
		return verdictModelIdentityStamp{
			Declared: declared,
			Family:   verdictModelFamily(declared),
			Basis:    verdictModelBasisWorkflowDisplay,
		}
	}
	if declared := declaredWorkflowString(lane["model"]); declared != "" {
		return verdictModelIdentityStamp{
			Declared: declared,
			Family:   verdictModelFamily(declared),
			Basis:    verdictModelBasisWorkflowModel,
		}
	}
	return verdictModelIdentityUnknown(verdictModelUnknown)
}

func workflowJobDefinitionFromWorkflow(workflow map[string]any, workflowJobID string) map[string]any {
	for _, item := range asList(workflow["jobs"]) {
		def := asMap(item)
		if fmt.Sprint(def["id"]) == workflowJobID {
			return def
		}
	}
	return map[string]any{}
}

func verdictIdentityLaneID(def map[string]any, job map[string]any) string {
	if laneID := declaredWorkflowString(def["lane_id"]); laneID != "" {
		return laneID
	}
	selector := asMap(job["lane_selector_json"])
	return declaredWorkflowString(selector["lane_id"])
}

func declaredWorkflowString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func verdictModelStringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func verdictModelFamily(value string) string {
	tokens := []string{}
	for _, token := range verdictModelTokenPattern.Split(strings.ToLower(value), -1) {
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return verdictModelUnknown
	}
	if (tokens[0] == "openai" || tokens[0] == "anthropic" || tokens[0] == "google") && len(tokens) > 1 {
		return tokens[1]
	}
	return tokens[0]
}

func verdictModelCoBlindness(ctx context.Context, runner any, repositoryID string, workflow map[string]any, job map[string]any, family string) string {
	if fmt.Sprint(job["job_type"]) != "review" {
		return verdictCoBlindnessNotReview
	}
	if family == "" || family == verdictModelUnknown {
		return verdictCoBlindnessUnknown
	}
	upstreams, err := queryRows(ctx, runner, `
		SELECT j.*
		  FROM striatumd.job_dependencies dep
		  JOIN striatumd.jobs j
		    ON j.repository_id = dep.repository_id
		   AND j.job_id = dep.depends_on_job_id
		 WHERE dep.repository_id = $1
		   AND dep.job_id = $2
		 ORDER BY j.created_at, j.job_id`, repositoryID, job["job_id"])
	if err != nil || len(upstreams) == 0 {
		return verdictCoBlindnessUnknown
	}
	sawKnown := false
	for _, upstream := range upstreams {
		upstreamStamp := normalizeVerdictModelIdentityStamp(verdictModelIdentityForWorkflowJob(workflow, upstream))
		if upstreamStamp.Family == verdictModelUnknown {
			return verdictCoBlindnessUnknown
		}
		sawKnown = true
		if upstreamStamp.Family == family {
			return verdictCoBlindnessSameFamily
		}
	}
	if sawKnown {
		return verdictCoBlindnessIndependent
	}
	return verdictCoBlindnessUnknown
}
