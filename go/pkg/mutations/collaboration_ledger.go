package mutations

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

// cyclePlaceholder is the literal token a generated workflow places in a
// cycle-scoped expected artifact's logical_name and path. Each revision cycle
// re-runs the same job row with a bumped `attempt`, so the daemon resolves the
// token to a distinct `cycle_<attempt>` segment before the artifact is
// surfaced, published, or verified.
//
// This is the runtime half of the RFC 0093 design synthesis §4.6 cycle_<N>
// naming convention: a `needs_revision` re-loop publishes the ledger under a
// new logical name + path so it does not collide with the prior cycle's
// content-hash guard (which would otherwise deadlock the revision cycle), while
// preserving every cycle's ledger as durable provenance instead of deleting the
// prior artifact rows.
const cyclePlaceholder = "${cycle}"

// cycleSegmentForAttempt maps a job attempt (1-based) to its cycle segment.
// Attempt 1 is the first dialogue round (cycle_1); attempt N is the N-th.
func cycleSegmentForAttempt(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return fmt.Sprintf("cycle_%d", attempt)
}

// resolveCyclePlaceholders substitutes the cycle placeholder in a string with
// the segment derived from the job attempt. Strings without the placeholder are
// returned unchanged, so this is safe to apply to every expected artifact.
func resolveCyclePlaceholders(value string, attempt int) string {
	if !strings.Contains(value, cyclePlaceholder) {
		return value
	}
	return strings.ReplaceAll(value, cyclePlaceholder, cycleSegmentForAttempt(attempt))
}

// resolveExpectedArtifactCycles returns a copy of the expected-artifact list
// with every cycle placeholder in logical_name and path resolved against the
// supplied attempt. The daemon calls this wherever the stored
// expected_artifacts_json is consumed (work-packet build, required-artifact
// verification, and the submit-review pre-check) so the published artifact's
// attempt-scoped logical name and path agree across every code path.
func resolveExpectedArtifactCycles(expected []any, attempt int) []any {
	result := make([]any, 0, len(expected))
	for _, item := range expected {
		artifact := asMap(item)
		if len(artifact) == 0 {
			result = append(result, item)
			continue
		}
		resolved := map[string]any{}
		for key, value := range artifact {
			switch key {
			case "logical_name", "path":
				if text, ok := value.(string); ok {
					resolved[key] = resolveCyclePlaceholders(text, attempt)
					continue
				}
			}
			resolved[key] = value
		}
		result = append(result, resolved)
	}
	return result
}

// enforceCollaborationLedgerVerdict closes the primitive-path bypass (build
// finding 2). The submit-review path already refuses a verdict that disagrees
// with the collaboration_ledger front matter, but the primitive path
// (publish_artifact then review.verdict / recordVerdict) only verified the
// expected artifact exists — it never parsed the ledger. An adjudicator could
// therefore publish a `verdict: needs_revision` ledger and then record
// `accept`, clearing the substance gate it is supposed to enforce.
//
// When any required expected artifact of the verdict-capable job is a
// collaboration_ledger, the recorded verdict must equal the ledger's
// front-matter verdict. The check reads the ledger from the run's repo root and
// resolves cycle placeholders against the job attempt, matching what the
// adjudicator was told to publish.
func enforceCollaborationLedgerVerdict(ctx context.Context, runner any, repositoryID string, job map[string]any, recordedVerdict string) error {
	attempt := intValue(job["attempt"])
	expected := resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), attempt)
	var ledgerPath string
	for _, item := range expected {
		artifact := asMap(item)
		if artifact["kind"] != "collaboration_ledger" {
			continue
		}
		if artifact["required"] != true {
			continue
		}
		if text, ok := artifact["path"].(string); ok && text != "" {
			ledgerPath = text
			break
		}
	}
	if ledgerPath == "" {
		return nil
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return err
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	// #484: resolve the ledger through the job's active worktree, mirroring the
	// worktree-aware publish path (artifactSourcePath in publishArtifactWithOptions)
	// and the fresh-review re-read (enforceFreshReviewNotByteIdentical). A
	// worktree-isolated adjudicate/review job publishes its ledger into the per-job
	// worktree, not repoRoot (checked out on the default branch); a bare
	// repoRelativePath read then fails with "collaboration_ledger artifact file does
	// not exist" and the verdict can never be recorded. activeWorktreeForJob returns
	// nil for a non-isolated job, so artifactSourcePath falls back to repoRoot.
	activeWorktree, err := activeWorktreeForJob(ctx, runner, repositoryID, fmt.Sprint(job["job_id"]))
	if err != nil {
		return err
	}
	resolved, err := artifactSourcePath(repoRoot, ledgerPath, activeWorktree)
	if err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	payload, err := os.ReadFile(resolved)
	if err != nil {
		return rpc.NewError("artifact_error", "collaboration_ledger artifact file does not exist", nil)
	}
	frontMatter, err := artifactcontracts.ParseAndValidateFrontMatter("collaboration_ledger", resolved, payload)
	if err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	ledgerVerdict := fmt.Sprint(frontMatter["verdict"])
	if ledgerVerdict != recordedVerdict {
		return rpc.NewError("artifact_error", fmt.Sprintf("recorded verdict %q must match collaboration_ledger front matter verdict %q", recordedVerdict, ledgerVerdict), nil)
	}
	return nil
}

// enforceConstraintDischarge implements the RFC 0098 slice 3 discharge-verifying
// final review (§1 / §5 invariant 4). It is a pure typecheck against
// already-published artifacts — it never re-runs or re-opens a prior phase.
//
// Trigger (CONSTRAINTS ON SIGNAL in the slice brief): the gate fires when the
// SUBMITTED artifact is a finding/findings_ledger that carries a
// `constraint_discharge[]` block AND the run has at least one cleared
// `adjudicated_constraint_extraction` collaboration_ledger with binding
// constraints. This "if you claim to discharge constraints, you must discharge
// ALL binding ones" signal is chosen over a job-row signal (role_id /
// phase_id), because the jobs table has no phase column — phase_id lives only
// in the workflow snapshot's per-job def — and because keying on the submitted
// content makes the gate robust to how the workflow was authored (generated ACE
// graph, hand-written graph, or the primitive publish path). The ACE
// final-review step generated by slice 2 publishes exactly this shape
// (kind=finding, logical_name=constraint_discharge), so it always trips the
// gate; a non-ACE finding that happens to omit constraint_discharge is never
// gated.
//
// It fails closed (exit code 6 / artifact_error) when any binding,
// final-review-required constraint is missing a discharge row, reported
// `missing`, or reported `partial` without being accepted as risk. It passes
// only when every such constraint is `discharged` or `accepted_risk` (with
// owner+stage, already enforced by ParseConstraintDischarge). The error names
// the offending constraint ids so the operator/agent can fix the table.
func enforceConstraintDischarge(ctx context.Context, runner any, repositoryID string, job map[string]any, kind, path string, payload []byte) error {
	if kind != "finding" && kind != "findings_ledger" {
		return nil
	}
	block, ok := artifactcontracts.FrontMatterBlock(string(payload))
	if !ok {
		return nil
	}
	frontMatter, err := artifactcontracts.ParseFrontMatterBlock(block)
	if err != nil {
		return nil
	}
	rawDischarge := frontMatter["constraint_discharge"]
	if rawDischarge == nil {
		// The finding does not claim to discharge constraints; not a final-review
		// discharge artifact, so there is nothing to typecheck.
		return nil
	}
	discharge, err := artifactcontracts.ParseConstraintDischarge(rawDischarge)
	if err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	binding, err := clearedACEBindingConstraints(ctx, runner, repositoryID, job)
	if err != nil {
		return err
	}
	if len(binding) == 0 {
		// No cleared ACE ledger with binding constraints to verify against. The
		// discharge table is still schema-valid (checked above); there is simply
		// nothing for the typecheck to enforce.
		return nil
	}
	dischargeByID := map[string]string{}
	for _, row := range discharge {
		dischargeByID[row.ConstraintID] = row.Status
	}
	undischarged := []string{}
	for _, constraint := range binding {
		if !constraint.FinalReviewRequired {
			continue
		}
		status, hasRow := dischargeByID[constraint.ID]
		switch {
		case !hasRow:
			undischarged = append(undischarged, fmt.Sprintf("%s (no discharge row)", constraint.ID))
		case status == "discharged" || status == "accepted_risk":
			// Pass. accepted_risk already required owner+stage at parse time.
		case status == "partial":
			undischarged = append(undischarged, fmt.Sprintf("%s (partial, not accepted as risk)", constraint.ID))
		case status == "missing":
			undischarged = append(undischarged, fmt.Sprintf("%s (missing)", constraint.ID))
		default:
			undischarged = append(undischarged, fmt.Sprintf("%s (%s)", constraint.ID, status))
		}
	}
	if len(undischarged) > 0 {
		sort.Strings(undischarged)
		return rpc.NewError("artifact_error", fmt.Sprintf(
			"final review fails closed: binding constraints not discharged: %s; mark each discharged or accepted_risk (with owner+stage) in constraint_discharge[]",
			strings.Join(undischarged, ", "),
		), nil)
	}
	return nil
}

// clearedACEBindingConstraints loads the latest cleared
// adjudicated_constraint_extraction collaboration_ledger for the job's run and
// returns its binding constraints. A ledger is "cleared" when its own
// front-matter verdict is `accept` or `accept_with_findings` (the substance
// gate already forces the recorded verdict to equal the ledger's front-matter
// verdict, so the front matter is authoritative). Ledgers are scanned in
// publish order (most recent first); the first clearing ACE ledger that yields
// binding constraints wins, matching RFC 0098 §1 "the latest cleared
// constraint ledger".
func clearedACEBindingConstraints(ctx context.Context, runner any, repositoryID string, job map[string]any) ([]artifactcontracts.BindingConstraint, error) {
	runID := fmt.Sprint(job["run_id"])
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", runID, false)
	if err != nil {
		return nil, err
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	rows, err := queryRows(ctx, runner, `
		SELECT repo_path
		  FROM striatumd.artifacts
		 WHERE repository_id = $1
		   AND run_id = $2
		   AND artifact_kind = 'collaboration_ledger'
		 ORDER BY created_at DESC, artifact_id DESC`, repositoryID, runID)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		repoPath := fmt.Sprint(row["repo_path"])
		resolved, err := repoRelativePath(repoRoot, repoPath, false)
		if err != nil {
			continue
		}
		ledgerBytes, err := os.ReadFile(resolved)
		if err != nil {
			continue
		}
		block, ok := artifactcontracts.FrontMatterBlock(string(ledgerBytes))
		if !ok {
			continue
		}
		parsed, err := artifactcontracts.ParseFrontMatterBlock(block)
		if err != nil {
			continue
		}
		if fmt.Sprint(parsed["shape"]) != "adjudicated_constraint_extraction" {
			continue
		}
		verdict := fmt.Sprint(parsed["verdict"])
		if verdict != "accept" && verdict != "accept_with_findings" {
			continue
		}
		binding := artifactcontracts.BindingConstraintsFromLedger(parsed)
		if len(binding) > 0 {
			return binding, nil
		}
	}
	return nil, nil
}
