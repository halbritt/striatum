package reads

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// HandleRunSummary mirrors reads/run_summary.py — a redaction-safe
// snapshot of the run's jobs, artifacts, verdicts, and a doctor block.
// In the Go port the doctor block calls HandleDoctor for parity with
// the Python implementation post-v1.52.0.
func HandleRunSummary(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "run.summary requires run_id", nil)
	}

	runs, err := collectRows(ctx, runner,
		`SELECT run_id, workflow_snapshot_id, repo_root, state, branch_name,
		        created_at, started_at, completed_at, stop_reason, completion_mode,
		        completion_record_json
		   FROM striatumd.runs WHERE repository_id = $1 AND run_id = $2`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found", nil)
	}
	// RFC 0118 P1-5: terminal runs project the frozen run_completion_record —
	// the sessions[] block is the last-live state captured before teardown,
	// not a live read that would see the post-teardown emptiness.
	completionRecord := objectOrEmpty(runs[0]["completion_record_json"])
	delete(runs[0], "completion_record_json")
	var sessionsBlock any
	if runTerminalState(fmt.Sprint(runs[0]["state"])) && completionRecord["sessions"] != nil {
		sessionsBlock = completionRecord["sessions"]
	} else {
		liveSessions, err := collectRows(ctx, runner,
			`SELECT session_id, role_id, lane_id, state, close_reason,
			        registered_at, closed_at
			   FROM striatumd.sessions
			  WHERE repository_id = $1 AND run_id = $2
			  ORDER BY registered_at, session_id`,
			repositoryID, runID,
		)
		if err != nil {
			return nil, err
		}
		sessionsBlock = liveSessions
	}

	jobs, err := collectRows(ctx, runner,
		`SELECT workflow_job_id, job_id, role_id, state, attempt
		   FROM striatumd.jobs
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY created_at`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}

	artifacts, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.job_id, a.artifact_kind AS kind, a.logical_name,
		        a.repo_path AS path, a.content_sha256, a.author_line AS byline,
		        a.created_at AS published_at`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1 AND a.run_id = $2
		  ORDER BY a.created_at`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(artifacts)
	decorateArtifactProvenance(artifacts)

	verdicts, err := collectRows(ctx, runner,
		`SELECT verdict_id, job_id, session_id, verdict,
		        posture AS review_posture, created_at AS recorded_at,
		        lane_attestation_at_record, review_provenance_override,
		        review_provenance_decision_id, supervisor_id_at_record
		   FROM striatumd.verdicts
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY created_at`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}

	// RFC 0118 P0-4: the override-cleared verdicts, with their authorizing
	// decisions, read from the frozen P0-1 stamps — the run's operator
	// overrides are auditable from the summary alone.
	overrides, err := collectRows(ctx, runner,
		`SELECT verdict_id, job_id, session_id, verdict, posture,
		        lane_attestation_at_record, review_provenance_decision_id,
		        created_at AS recorded_at
		   FROM striatumd.verdicts
		  WHERE repository_id = $1 AND run_id = $2
		    AND (review_provenance_override = true OR posture = 'override')
		  ORDER BY created_at`,
		repositoryID, runID,
	)
	if err != nil {
		return nil, err
	}

	doctor, err := HandleDoctor(ctx, runner, envelope)
	if err != nil {
		doctor = map[string]any{"ok": false, "error": err.Error()}
	}
	workflow, err := statusWorkflowForRun(ctx, runner, repositoryID, runID)
	if err != nil {
		return nil, err
	}

	// RFC 0132 P4b (#342): the finalize-decision legibility block — live (current-seal)
	// dissent_ledger rows blocking finalize, and any open advisory-guard hold on a gate
	// — so a quorum hold or an advisory park is self-explaining in run.summary before an
	// operator runs checkpoint resolve.
	quorumDissent := quorumDissentSummary(ctx, runner, repositoryID, runID)

	// RFC 0131 131-D (#337): the confidence-gate escalation-decision legibility block —
	// per-job confidence-gate state (consecutive_silent_sweeps / cap / misfire score /
	// last_probe_basis) for any deadline_elapsed_only pipe lane currently being
	// debounced or already escalated, so an operator sees WHY a lane is/isn't escalated
	// rather than silence.
	recoveryGate := recoveryGateSummary(ctx, runner, repositoryID, runID)

	result := map[string]any{
		"run":            runs[0],
		"jobs":           jobs,
		"artifacts":      artifacts,
		"verdicts":       verdicts,
		"overrides":      overrides,
		"sessions":       sessionsBlock,
		"doctor":         doctor,
		"operator_mode":  workflowOperatorMode(workflow),
		"quorum_dissent": quorumDissent,
		"recovery_gate":  recoveryGate,
	}
	if len(completionRecord) > 0 {
		result["completion_record"] = completionRecord
	}
	return result, nil
}

func runTerminalState(state string) bool {
	switch state {
	case "completed", "failed", "canceled", "compromised":
		return true
	}
	return false
}

// HandleEvidenceExport mirrors reads/evidence_export.py — a redacted
// Markdown export written under the target repository.
func HandleEvidenceExport(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	runID := stringParam(envelope, "run_id")
	pathText := stringParam(envelope, "path")
	if runID == "" {
		return nil, rpc.NewError("schema_invalid", "evidence.export requires run_id", nil)
	}
	if pathText == "" {
		return nil, rpc.NewError("schema_invalid", "evidence.export requires path", nil)
	}
	repoRoot, err := archiveRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, err
	}
	target, err := safeEvidenceOutputPath(repoRoot, pathText)
	if err != nil {
		return nil, err
	}
	runs, err := collectRows(ctx, runner,
		`SELECT run_id, state, branch_name, completed_at, completion_mode,
		        completion_record_json
		   FROM striatumd.runs
		  WHERE repository_id = $1 AND run_id = $2`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	if len(runs) == 0 {
		return nil, rpc.NewError("not_found", "run not found: "+runID, nil)
	}
	completionRecord := objectOrEmpty(runs[0]["completion_record_json"])
	delete(runs[0], "completion_record_json")
	overrideVerdicts, err := collectRows(ctx, runner,
		`SELECT verdict_id, job_id, verdict, posture,
		        lane_attestation_at_record, review_provenance_decision_id
		   FROM striatumd.verdicts
		  WHERE repository_id = $1 AND run_id = $2
		    AND (review_provenance_override = true OR posture = 'override')
		  ORDER BY created_at, verdict_id`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	artifacts, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.job_id, a.artifact_kind AS kind,
		        a.logical_name, a.repo_path AS path, a.content_sha256,
		        a.author_line AS byline, a.created_at AS published_at`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1 AND a.run_id = $2
		  ORDER BY a.created_at DESC LIMIT 500`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(artifacts)
	decorateArtifactProvenance(artifacts)
	verdicts, err := collectRows(ctx, runner,
		`SELECT verdict_id, run_id, job_id, verdict,
		        posture AS review_posture, created_at AS recorded_at
		   FROM striatumd.verdicts
		  WHERE repository_id = $1 AND run_id = $2
		  ORDER BY created_at DESC LIMIT 500`,
		repositoryID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	doctor, _ := HandleDoctor(ctx, runner, envelope)
	payload := redactEvidencePayload(map[string]any{
		"runs":      runs,
		"artifacts": artifacts,
		"verdicts":  verdicts,
		"doctor":    doctor,
	})
	body, err := renderEvidenceMarkdown(runs[0], payload)
	if err != nil {
		return nil, err
	}
	body += renderProvenanceSections(completionRecord, overrideVerdicts)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, err
	}
	meta, err := writeArchiveFile(target, []byte(body))
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":    "exported",
		"run_id":    runID,
		"path":      pathText,
		"sha256":    meta["sha256"],
		"bytes":     meta["bytes"],
		"runs":      payload["runs"],
		"artifacts": payload["artifacts"],
		"verdicts":  payload["verdicts"],
		"doctor":    payload["doctor"],
	}, nil
}

func safeEvidenceOutputPath(repoRoot string, pathText string) (string, error) {
	if filepath.IsAbs(pathText) {
		return "", rpc.NewError("path_outside_scope", "path must be repo-relative", nil)
	}
	repoResolved, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(repoResolved, pathText))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(repoResolved, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", rpc.NewError("path_outside_scope", "path must stay inside the repository", nil)
	}
	if rel == ".striatum" || strings.HasPrefix(rel, ".striatum"+string(os.PathSeparator)) {
		return "", rpc.NewError("path_outside_scope", "path must not be under .striatum", nil)
	}
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		return "", rpc.NewError("path_outside_scope", "--path must be a file", nil)
	}
	return target, nil
}

func renderEvidenceMarkdown(run map[string]any, payload map[string]any) (string, error) {
	payloadJSON, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Striatum Evidence Export\n\n")
	_, _ = fmt.Fprintf(&b, "- run_id: `%s`\n", fmt.Sprint(run["run_id"]))
	_, _ = fmt.Fprintf(&b, "- state: `%s`\n", fmt.Sprint(run["state"]))
	if branch := fmt.Sprint(run["branch_name"]); branch != "" && branch != "<nil>" {
		_, _ = fmt.Fprintf(&b, "- branch: `%s`\n", branch)
	}
	b.WriteString("\n## Snapshot\n\n")
	b.WriteString("```json\n")
	b.Write(payloadJSON)
	b.WriteString("\n```\n")
	return b.String(), nil
}

// renderProvenanceSections appends the RFC 0118 P1-5 deterministic sections.
// Sessions and the provenance-gate ledger come from the frozen
// run_completion_record (last-live state); operator overrides come from the
// frozen verdict stamps. Only identifiers, states, and category tokens are
// rendered — no prose fields, so no redaction pass is required.
func renderProvenanceSections(record map[string]any, overrides []map[string]any) string {
	var b strings.Builder
	b.WriteString("\n## Sessions\n\n")
	sessions, _ := record["sessions"].([]any)
	if len(sessions) == 0 {
		b.WriteString("- no frozen session record (pre-RFC-0118 terminal run or non-terminal run)\n")
	}
	for _, item := range sessions {
		session, _ := item.(map[string]any)
		line := fmt.Sprintf("- `%s` state=`%s` close_reason=`%s`",
			fmt.Sprint(session["session_id"]), fmt.Sprint(session["state"]), fmt.Sprint(session["close_reason"]))
		if attestation, ok := session["attestation"].(map[string]any); ok {
			line += fmt.Sprintf(" attestation=`%s`", fmt.Sprint(attestation["state"]))
			if supervisor := attestation["supervisor_id"]; supervisor != nil {
				line += fmt.Sprintf(" supervisor=`%s`", fmt.Sprint(supervisor))
			}
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n## Provenance Gate\n\n")
	gates, _ := record["provenance_gate"].([]any)
	if len(gates) == 0 {
		b.WriteString("- no frozen provenance ledger (pre-RFC-0118 terminal run or non-terminal run)\n")
	}
	for _, item := range gates {
		gate, _ := item.(map[string]any)
		line := fmt.Sprintf("- gate `%s` basis=`%s`",
			fmt.Sprint(gate["workflow_job_id"]), fmt.Sprint(gate["basis"]))
		if decision := gate["override_decision_id"]; decision != nil {
			line += fmt.Sprintf(" decision=`%s`", fmt.Sprint(decision))
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n## Operator Overrides\n\n")
	if len(overrides) == 0 {
		b.WriteString("- none\n")
	}
	for _, row := range overrides {
		line := fmt.Sprintf("- verdict `%s` on job `%s`: `%s` posture=`%s` lane_at_record=`%s`",
			fmt.Sprint(row["verdict_id"]), fmt.Sprint(row["job_id"]), fmt.Sprint(row["verdict"]),
			fmt.Sprint(row["posture"]), fmt.Sprint(row["lane_attestation_at_record"]))
		if decision := row["review_provenance_decision_id"]; decision != nil {
			line += fmt.Sprintf(" decision=`%s`", fmt.Sprint(decision))
		}
		b.WriteString(line + "\n")
	}
	// RFC 0134 / D227: the executable-half claim-verification ledger frozen on the
	// run_completion_record. Each row names the authored vs. EFFECTIVE claim status
	// the daemon's pure-read gate derived from the sealed receipts (VERIFIED only
	// for a two-signal strict-sandbox receipt bound to the claim's inputs; ASSERTED
	// otherwise, with the degrade basis). Only identifiers and status/basis tokens
	// are rendered — no prose — so no redaction pass is required.
	b.WriteString("\n## Claim Verification\n\n")
	claims, _ := record["claim_verification"].([]any)
	if len(claims) == 0 {
		b.WriteString("- no frozen claim ledger (run published no claim_ledger, or pre-RFC-0134 terminal run)\n")
	}
	for _, item := range claims {
		claim, _ := item.(map[string]any)
		line := fmt.Sprintf("- claim `%s` authored=`%s` effective=`%s`",
			fmt.Sprint(claim["claim_id"]), fmt.Sprint(claim["authored_status"]), fmt.Sprint(claim["effective_status"]))
		if basis := claim["verification_basis"]; basis != nil {
			line += fmt.Sprintf(" basis=`%s`", fmt.Sprint(basis))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// HandleCorpusExport exposes the redaction-safe corpus rows the augmentation
// contract consumes. The Go path owns the active redaction-tier validation and
// field-level redaction rules for the daemon-backed artifact projection.
func HandleCorpusExport(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	redactionTier, err := redactionTierFromEnvelope(envelope.Params)
	if err != nil {
		return nil, rpc.NewError("schema_invalid", err.Error(), nil)
	}
	limit, count := limitClause(envelope, 1000)
	rows, err := collectRows(ctx, runner,
		`SELECT a.artifact_id, a.run_id, a.artifact_kind AS kind, a.logical_name,
		        a.repo_path AS path, a.content_sha256, a.author_line AS byline,
		        a.created_at AS published_at`+artifactPlacementProjection(ctx, runner, "a")+artifactProvenanceColumns+`
		   FROM striatumd.artifacts a`+artifactProvenanceJoins+`
		  WHERE a.repository_id = $1
		  ORDER BY a.created_at DESC`+limit,
		repositoryID,
	)
	if err != nil {
		return nil, err
	}
	decorateArtifactPlacements(rows)
	decorateArtifactProvenance(rows)
	redactedRows := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		redactedRows = append(redactedRows, redactCorpusArtifactRow(row))
	}
	result := map[string]any{
		"corpus_contract_version": 2,
		"repository_id":           repositoryID,
		"redaction_tier":          redactionTier,
		"row_count":               len(redactedRows),
		"limit":                   count,
		"rows":                    redactedRows,
	}
	if boolFromParams(envelope.Params, "include_lane_trajectory") {
		trajectory, err := laneTrajectoryCorpus(ctx, runner, repositoryID, stringParam(envelope, "run_id"), redactionTier, count)
		if err != nil {
			return nil, err
		}
		result["classes"] = []string{"artifacts", "lane_trajectory"}
		result["lane_trajectory"] = trajectory
	}
	return result, nil
}

func redactCorpusArtifactRow(row map[string]any) map[string]any {
	redacted := map[string]any{}
	for key, value := range row {
		redacted[key] = value
	}
	if path, ok := redacted["path"].(string); ok {
		if err := validateCorpusSourcePath(path); err != nil {
			redacted["path"] = evidenceFreeTextPlaceholder
		}
	}
	for _, key := range []string{"content", "body", "rationale", "description", "payload_json", "metadata_json"} {
		if redacted[key] != nil {
			redacted[key] = evidenceFreeTextPlaceholder
		}
	}
	return redacted
}

func laneTrajectoryCorpus(ctx context.Context, runner db.Runner, repositoryID, runID, redactionTier string, limit int) (map[string]any, error) {
	runIDs := []string{}
	if strings.TrimSpace(runID) != "" {
		runIDs = append(runIDs, strings.TrimSpace(runID))
	} else {
		rows, err := collectRows(ctx, runner,
			`SELECT run_id
			   FROM striatumd.runs
			  WHERE repository_id = $1
			  ORDER BY created_at DESC, run_id ASC
			  LIMIT $2`,
			repositoryID,
			limit,
		)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			runIDs = append(runIDs, fmt.Sprint(row["run_id"]))
		}
		sort.Strings(runIDs)
	}

	corpusRows := make([]map[string]any, 0)
	for _, id := range runIDs {
		records, err := fetchTrajectory(ctx, runner, repositoryID, id, "provenance", 0)
		if err != nil {
			return nil, err
		}
		corpusRows = append(corpusRows, laneTrajectoryRowsFromRecords(id, redactionTier, records)...)
	}
	sort.SliceStable(corpusRows, func(i, j int) bool {
		return laneTrajectorySortKey(corpusRows[i]) < laneTrajectorySortKey(corpusRows[j])
	})
	payload, err := json.Marshal(corpusRows)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return map[string]any{
		"schema_version": "striatum.corpus.lane_trajectory.v1",
		"source_class":   "lane_trajectory",
		"row_count":      len(corpusRows),
		"sha256":         fmt.Sprintf("%x", sum[:]),
		"rows":           corpusRows,
	}, nil
}

func laneTrajectoryRowsFromRecords(runID, redactionTier string, records []map[string]any) []map[string]any {
	rows := make([]map[string]any, 0, len(records))
	for index, record := range records {
		body, _ := record["body"].(map[string]any)
		text := laneTrajectoryText(body)
		eventFields := laneTrajectoryEventFields(body)
		rows = append(rows, map[string]any{
			"schema_version": "striatum.corpus.lane_trajectory.row.v1",
			"source_class":   "lane_trajectory",
			"run_id":         runID,
			"session_id":     fmt.Sprint(record["session_id"]),
			"role_id":        fmt.Sprint(record["role_id"]),
			"lane_id":        fmt.Sprint(record["lane_id"]),
			"ordinal":        index + 1,
			"event_kind":     fmt.Sprint(record["kind"]),
			"created_at":     fmt.Sprint(record["ts"]),
			"redaction_tier": redactionTier,
			"text":           text,
			"event_fields":   eventFields,
		})
	}
	return rows
}

func laneTrajectoryText(body map[string]any) string {
	if body == nil {
		return ""
	}
	if text, ok := body["text"].(string); ok {
		return redactGeneratedCorpusText(text)
	}
	if value, ok := body["text"]; ok {
		if encoded, err := json.Marshal(value); err == nil {
			return redactGeneratedCorpusText(string(encoded))
		}
	}
	return ""
}

func laneTrajectoryEventFields(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{}
	}
	allowed := map[string]bool{
		"artifact_id":      true,
		"blocker_id":       true,
		"interrogation_id": true,
		"kind":             true,
		"logical_name":     true,
		"message_kind":     true,
		"path":             true,
		"placement":        true,
		"posture":          true,
		"severity":         true,
		"state":            true,
		"topic":            true,
		"turn":             true,
		"turn_index":       true,
		"verdict":          true,
		"verdict_id":       true,
	}
	fields := map[string]any{}
	for _, key := range sortedMapKeys(body) {
		if !allowed[key] && !safeEventFields[key] {
			continue
		}
		value := body[key]
		if key == "path" {
			path := fmt.Sprint(value)
			if err := validateCorpusSourcePath(path); err != nil {
				fields[key] = evidenceFreeTextPlaceholder
				continue
			}
		}
		fields[key] = redactCorpusScalar(value)
	}
	return fields
}

func redactCorpusScalar(value any) any {
	switch typed := value.(type) {
	case string:
		return redactGeneratedCorpusText(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactCorpusScalar(item))
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for _, key := range sortedMapKeys(typed) {
			if safeEventFields[key] {
				out[key] = redactCorpusScalar(typed[key])
			}
		}
		return out
	default:
		return value
	}
}

func laneTrajectorySortKey(row map[string]any) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%08d",
		fmt.Sprint(row["run_id"]),
		fmt.Sprint(row["session_id"]),
		fmt.Sprint(row["created_at"]),
		fmt.Sprint(row["event_kind"]),
		intFrom(row, "ordinal"),
	)
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func boolFromParams(params map[string]any, key string) bool {
	switch value := params[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true") || value == "1" || strings.EqualFold(value, "yes")
	default:
		return false
	}
}
