package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
)

const recoveryVerdictArtifactBasis = "daemon_auto_finalized_from_published_artifact"

type recoveryVerdictArtifact struct {
	ArtifactID    string
	LogicalName   string
	Kind          string
	RepoPath      string
	SessionID     string
	LeaseID       string
	Verdict       string
	ContentSHA256 string
	Source        string
}

// finalizeVerdictCapableJobFromDurableArtifact is the verdict-capable twin of
// finalizeStalledJob: it never marks a review/phase_synthesis job completed
// directly. Instead it validates the already-published required artifact body,
// derives the lane's recorded verdict from front matter, and routes through
// applyVerdict with explicit recovery provenance.
func finalizeVerdictCapableJobFromDurableArtifact(ctx context.Context, tx db.TxRunner, repositoryID string, job map[string]any, summary string) (map[string]any, error) {
	if !isVerdictCapableJobType(fmt.Sprint(job["job_type"])) {
		return nil, rpc.NewError("invalid_transition", "durable verdict-artifact recovery applies only to verdict-capable jobs", nil)
	}
	jobID := fmt.Sprint(job["job_id"])
	runID := fmt.Sprint(job["run_id"])
	artifact, recon, err := recoverableVerdictArtifact(ctx, tx, repositoryID, job)
	if err != nil {
		return nil, err
	}
	reason := "autonomous recovery: auto-finalized published-but-unsealed verdict-capable job from durable artifact"
	if summary != "" {
		reason += ": " + summary
	}
	reviewProvenance := map[string]any{
		"review_provenance_override": true,
		"review_provenance_basis":    recoveryVerdictArtifactBasis,
	}
	completeResult, err := applyVerdict(ctx, tx, repositoryID, artifact.SessionID, jobID, artifact.LeaseID, artifact.Verdict, job, artifact.ArtifactID, reason, reviewProvenance, nil)
	if err != nil {
		return nil, err
	}
	now := nowString()
	if err := resolveAutonomousBlockersOnCompletion(ctx, tx, repositoryID, runID, jobID, artifact.SessionID, now); err != nil {
		return nil, err
	}
	if err := maybeCompleteRun(ctx, tx, repositoryID, runID); err != nil {
		return nil, err
	}
	return map[string]any{
		"status":        "completed",
		"job_id":        jobID,
		"verdict":       artifact.Verdict,
		"artifact_id":   artifact.ArtifactID,
		"logical_name":  artifact.LogicalName,
		"artifact_kind": artifact.Kind,
		"repo_path":     artifact.RepoPath,
		"source":        artifact.Source,
		"completion":    completeResult,
		"artifacts":     reconstructionLedgerEntries(recon),
	}, nil
}

func recoverableVerdictArtifact(ctx context.Context, runner any, repositoryID string, job map[string]any) (recoveryVerdictArtifact, []artifactReconstruction, error) {
	jobID := fmt.Sprint(job["job_id"])
	if err := verifyRequiredArtifacts(ctx, runner, repositoryID, jobID); err != nil {
		return recoveryVerdictArtifact{}, nil, err
	}
	recon, err := verifyRequiredArtifactReconstructable(ctx, runner, repositoryID, job)
	if err != nil {
		return recoveryVerdictArtifact{}, nil, err
	}
	if failed := failedReconstructions(recon); len(failed) > 0 {
		return recoveryVerdictArtifact{}, recon, rpc.NewError("invalid_transition",
			"verdict-artifact recovery refused: a required artifact body is not reconstructable from its declared placement",
			map[string]any{
				"run_id":            job["run_id"],
				"job_id":            jobID,
				"unreconstructable": reconstructionLedgerEntries(failed),
			})
	}

	attempt := jobAttemptValue(job["attempt"])
	for _, item := range resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), attempt) {
		expected := asMap(item)
		if expected["required"] != true {
			continue
		}
		kind := fmt.Sprint(expected["kind"])
		if kind != "finding" && kind != "synthesis" {
			continue
		}
		artifact, err := recoveryVerdictArtifactRow(ctx, runner, repositoryID, jobID, expected, attempt)
		if err != nil {
			return recoveryVerdictArtifact{}, recon, err
		}
		body, source, err := recoveryArtifactBody(ctx, runner, repositoryID, job, artifact)
		if err != nil {
			return recoveryVerdictArtifact{}, recon, err
		}
		frontMatter, err := artifactcontracts.ParseAndValidateFrontMatter(kind, fmt.Sprint(artifact["repo_path"]), body)
		if err != nil {
			return recoveryVerdictArtifact{}, recon, rpc.NewError("artifact_error", err.Error(), nil)
		}
		verdict, err := recoveryVerdictFromFrontMatter(kind, frontMatter)
		if err != nil {
			return recoveryVerdictArtifact{}, recon, err
		}
		sessionID := reconstructionText(artifact["session_id"])
		if sessionID == "" {
			return recoveryVerdictArtifact{}, recon, rpc.NewError("invalid_transition", "verdict-artifact recovery requires the published artifact's session_id", nil)
		}
		return recoveryVerdictArtifact{
			ArtifactID:    fmt.Sprint(artifact["artifact_id"]),
			LogicalName:   fmt.Sprint(artifact["logical_name"]),
			Kind:          kind,
			RepoPath:      fmt.Sprint(artifact["repo_path"]),
			SessionID:     sessionID,
			LeaseID:       reconstructionText(artifact["lease_id"]),
			Verdict:       verdict,
			ContentSHA256: fmt.Sprint(artifact["content_sha256"]),
			Source:        source,
		}, recon, nil
	}
	return recoveryVerdictArtifact{}, recon, rpc.NewError("invalid_transition", "verdict-artifact recovery requires a required finding or synthesis artifact with verdict front matter", nil)
}

func recoveryVerdictArtifactRow(ctx context.Context, runner any, repositoryID, jobID string, expected map[string]any, attempt int) (map[string]any, error) {
	row, err := oneRow(ctx, runner, `
		SELECT a.artifact_id, a.session_id, a.logical_name, a.artifact_kind,
		       a.repo_path, a.content_sha256, a.blob_key, a.blob_sha256,
		       a.attempt,
		       (
		         SELECT e.lease_id
		           FROM striatumd.events e
		          WHERE e.repository_id = a.repository_id
		            AND e.artifact_id = a.artifact_id
		            AND e.event_type = 'artifact.published'
		          ORDER BY e.event_id DESC
		          LIMIT 1
		       ) AS lease_id
		  FROM striatumd.artifacts a
		 WHERE a.repository_id = $1 AND a.job_id = $2 AND a.logical_name = $3
		   AND a.artifact_kind = $4 AND a.repo_path = $5 AND a.attempt = $6
		 ORDER BY a.created_at DESC, a.artifact_id DESC
		 LIMIT 1`,
		repositoryID, jobID, expected["logical_name"], expected["kind"], expected["path"], attempt)
	if err != nil {
		if errorsIsNoRows(err) {
			return nil, rpc.NewError("invalid_transition", fmt.Sprintf(
				"required verdict artifact is missing: logical_name=%q, kind=%q, path=%q",
				expected["logical_name"], expected["kind"], expected["path"]), nil)
		}
		return nil, err
	}
	return row, nil
}

func recoveryArtifactBody(ctx context.Context, runner any, repositoryID string, job map[string]any, artifact map[string]any) ([]byte, string, error) {
	contentSHA := reconstructionText(artifact["content_sha256"])
	if contentSHA == "" {
		return nil, "", rpc.NewError("artifact_error", "published artifact is missing content_sha256", nil)
	}
	if blobKey := reconstructionText(artifact["blob_key"]); blobKey != "" {
		if packageBlobClient == nil {
			return nil, "", rpc.NewError("blob_disabled", "published artifact references blob storage but the daemon has no blob client", map[string]any{"blob_key": blobKey})
		}
		bucket, err := lookupRepoBlobBucket(ctx, runner, repositoryID)
		if err != nil {
			return nil, "", err
		}
		if bucket == "" {
			return nil, "", rpc.NewError("blob_disabled", "published artifact references blob storage but the repository has no blob bucket", map[string]any{"blob_key": blobKey})
		}
		body, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, contentSHA)
		if err != nil {
			return nil, "", rpc.NewError("blob_read_failed", err.Error(), map[string]any{"blob_key": blobKey})
		}
		return body, "blob", nil
	}
	body, source, err := recoveryArtifactBodyFromGit(ctx, runner, repositoryID, job, artifact, contentSHA)
	if err != nil {
		return nil, "", err
	}
	return body, source, nil
}

func recoveryArtifactBodyFromGit(ctx context.Context, runner any, repositoryID string, job map[string]any, artifact map[string]any, contentSHA string) ([]byte, string, error) {
	repoRoot, err := activeRepositoryRoot(ctx, runner, repositoryID)
	if err != nil {
		return nil, "", err
	}
	runID := fmt.Sprint(job["run_id"])
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", runID, false)
	if err != nil {
		return nil, "", err
	}
	repoPath, ok := cleanDurableArtifactPath(fmt.Sprint(artifact["repo_path"]))
	if !ok {
		return nil, "", rpc.NewError("artifact_error", "published artifact repo_path is invalid", map[string]any{"repo_path": artifact["repo_path"]})
	}
	jobID := fmt.Sprint(job["job_id"])
	attempt := jobAttemptValue(job["attempt"])
	refs := dedupRefs(append(append([]string{attemptPinRef(runID, jobID, attempt)}, gitJobAnchorRefs(ctx, repoRoot, runID, jobID)...), legacyPinRef(runID, jobID)))
	if branch := strings.TrimSpace(fmt.Sprint(run["branch_name"])); branch != "" {
		refs = append(refs, "refs/heads/"+branch)
	}
	for _, ref := range refs {
		if !gitRefExists(ctx, repoRoot, ref) {
			continue
		}
		result, err := runGitWorktreeCommand(ctx, repoRoot, "show", ref+":"+repoPath)
		if err != nil {
			return nil, "", err
		}
		if result.ExitCode != 0 {
			continue
		}
		body := []byte(result.Stdout)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) == contentSHA {
			return body, "git_anchor:" + ref, nil
		}
	}
	return nil, "", rpc.NewError("artifact_error", "published artifact body could not be read back from blob storage or durable git refs", map[string]any{
		"artifact_id":    artifact["artifact_id"],
		"repo_path":      artifact["repo_path"],
		"content_sha256": contentSHA,
	})
}

func recoveryVerdictFromFrontMatter(kind string, frontMatter map[string]any) (string, error) {
	field := "verdict_intent"
	if kind == "synthesis" {
		field = "status"
	}
	raw, ok := frontMatter[field].(string)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", rpc.NewError("artifact_error", fmt.Sprintf("%s artifact front matter must carry %s for verdict recovery", kind, field), nil)
	}
	verdict := normalizeVerdict(raw)
	switch verdict {
	case "accept", "accept_with_findings", "needs_revision", "reject":
		return verdict, nil
	default:
		return "", rpc.NewError("artifact_error", fmt.Sprintf("%s artifact front matter %s=%q is not a recoverable verdict", kind, field, raw), nil)
	}
}
