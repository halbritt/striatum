package mutations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/blob"
	"github.com/halbritt/striatum/go/pkg/db"
	"github.com/halbritt/striatum/go/pkg/rpc"
	"github.com/jackc/pgx/v5"
)

var allowedArtifactKinds = artifactcontracts.AllowedKindSet()
var frontMatterSchemas = artifactcontracts.SchemaSet()

// artifactLogicalNameConflictMessage is the operator-facing guidance returned
// when a still-running job tries to re-publish an existing logical_name with
// different content (#94). An already-published artifact is immutable for this
// attempt: there is no in-attempt supersession path. The fix is to publish a
// distinct logical_name (the job's already-published artifact is the one used
// at work.complete), or to let a revision cycle re-open the job so a fresh
// attempt publishes its own attempt-scoped artifact. True mutation/
// supersession across a revision cycle is tracked by the append-only,
// attempt-scoped artifact work in #84 / RFC 0095.
const artifactLogicalNameConflictMessage = "artifact logical_name already exists with different content; an already-published artifact is immutable for this attempt. Publish a distinct logical_name (the job's existing published artifact is the one used at work.complete), or let a revision cycle re-open the job so a fresh attempt can publish its own attempt-scoped artifact; revising a published artifact across a revision cycle is tracked by attempt-scoped artifacts (#84 / RFC 0095)."

// jobAttemptValue normalizes a jobs.attempt column value to the producing
// attempt for an artifact. jobs.attempt is NOT NULL DEFAULT 1, but a missing or
// zero value (e.g. a legacy row read before migration, or a partially seeded
// test fixture) defaults to 1 — the same value migration 0018 backfills onto
// pre-existing artifact rows — so the attempt-scoped keys stay coherent.
func jobAttemptValue(value any) int {
	if attempt := intValue(value); attempt > 0 {
		return attempt
	}
	return 1
}

func HandlePublishArtifact(ctx context.Context, runner db.Runner, envelope rpc.Envelope) (map[string]any, error) {
	repositoryID, err := requireRepositoryID(envelope)
	if err != nil {
		return nil, err
	}
	sessionID := stringParam(envelope, "session_id")
	jobID := stringParam(envelope, "job_id")
	leaseID := stringParam(envelope, "lease_id")
	kind := stringParam(envelope, "kind")
	logicalName := stringParam(envelope, "logical_name")
	pathText := stringParam(envelope, "path")
	if sessionID == "" || jobID == "" || leaseID == "" || kind == "" || logicalName == "" || pathText == "" {
		return nil, rpc.NewError("schema_invalid", "artifact.publish requires session_id, job_id, lease_id, kind, logical_name, and path", nil)
	}
	// RFC 0096 V2 / #135: a session-bound capability token may only publish as its
	// own session; an unbound operator/coordinator token may publish for any
	// session (override). Closes the cross-session publish spoof — a bound caller
	// presenting another session's (observable) lease_id is now refused on receipt
	// before the lease-ownership check.
	return withTx(ctx, runner, func(tx db.TxRunner) (map[string]any, error) {
		if _, err := enforceSessionBindingForSession(ctx, tx, repositoryID, sessionID, "artifact.publish"); err != nil {
			return nil, err
		}
		if err := enforceActiveActingSession(ctx, tx, repositoryID, sessionID, jobID, "artifact.publish"); err != nil {
			return nil, err
		}
		return publishArtifact(ctx, tx, repositoryID, sessionID, jobID, leaseID, kind, logicalName, pathText)
	})
}

func publishArtifact(
	ctx context.Context,
	runner any,
	repositoryID string,
	sessionID string,
	jobID string,
	leaseID string,
	kind string,
	logicalName string,
	pathText string,
) (map[string]any, error) {
	return publishArtifactWithOptions(ctx, runner, repositoryID, sessionID, jobID, leaseID, kind, logicalName, pathText, publishArtifactOptions{})
}

type publishArtifactOptions struct {
	ReviewProvenanceOverride bool
}

func publishArtifactWithOptions(
	ctx context.Context,
	runner any,
	repositoryID string,
	sessionID string,
	jobID string,
	leaseID string,
	kind string,
	logicalName string,
	pathText string,
	options publishArtifactOptions,
) (map[string]any, error) {
	job, err := rowByID(ctx, runner, repositoryID, "jobs", "job_id", jobID, true)
	if err != nil {
		return nil, err
	}
	if _, err := activeLeaseFor(ctx, runner, repositoryID, leaseID, sessionID, jobID); err != nil {
		return nil, err
	}
	if kind == "transcript" {
		return nil, rpc.NewError("artifact_error", "transcript artifacts are not allowed by default", nil)
	}
	if !allowedArtifactKinds[kind] {
		return nil, rpc.NewError("artifact_error", fmt.Sprintf("artifact kind %q is not in the allowed kinds list", kind), nil)
	}
	if err := enforceRequiredAttestationForArtifactPublishWithOverride(ctx, runner, repositoryID, job, sessionID, options.ReviewProvenanceOverride); err != nil {
		return nil, err
	}
	run, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return nil, err
	}
	repoRoot := fmt.Sprint(run["repo_root"])
	applyFrozenAttemptWriteScope(ctx, runner, repositoryID, job, leaseID)
	writeScope := asMap(job["write_scope_json"])
	if !pathAllowed(repoRoot, pathText, writeScope) {
		return nil, writeScopePathError(job, pathText, stringListFromAny(writeScope["allowed_paths"]), stringListFromAny(writeScope["forbidden_paths"]))
	}
	activeWorktree, err := requireActiveWorktreeForJob(ctx, runner, repositoryID, job, "artifact.publish")
	if err != nil {
		return nil, err
	}
	path, err := artifactSourcePath(repoRoot, pathText, activeWorktree)
	if err != nil {
		return nil, rpc.NewError("artifact_error", err.Error(), nil)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, rpc.NewError("artifact_error", "artifact file does not exist", nil)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	payload, err = ensureRequiredFrontMatter(kind, path, payload)
	if err != nil {
		return nil, err
	}
	if err := validateMarkdownAuthorLine(ctx, runner, repositoryID, job, sessionID, path, payload); err != nil {
		return nil, err
	}
	if err := validateArtifactFrontMatter(kind, path, payload); err != nil {
		return nil, err
	}
	// RFC 0098 slice 3: discharge-verifying final review. When a finding /
	// findings_ledger carries a constraint_discharge[] table and the run has a
	// cleared ACE collaboration_ledger with binding constraints, every binding
	// (final-review-required) constraint must be discharged or accepted_risk.
	// Fails closed on any missing / unaccepted-partial. This is a pure typecheck
	// against already-published artifacts; it fires on both artifact.publish and
	// review.submit (which routes through publishArtifact).
	if err := enforceConstraintDischarge(ctx, runner, repositoryID, job, kind, path, payload); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	now := nowString()
	// RFC 0095 §1 / #84: artifacts are attempt-scoped. A re-opened job (revision
	// cycle, run.retry_job, checkpoint continue) bumps jobs.attempt; the prior
	// attempt's row stays for provenance and must NOT collide with the fresh
	// attempt's republish of the same logical_name. So the collision pre-check is
	// keyed on the CURRENT attempt: only a row at this attempt with the same
	// logical_name and DIFFERENT content is a conflict; a prior attempt's row is
	// ignored and the fresh attempt INSERTs its own attempt-scoped row.
	attempt := jobAttemptValue(job["attempt"])
	placement := resolvePublishPlacement(job, kind, logicalName, pathText, attempt)
	existing, err := oneRow(ctx, runner, `
		SELECT * FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND job_id = $3 AND logical_name = $4
		   AND attempt = $5
		 LIMIT 1`, repositoryID, job["run_id"], jobID, logicalName, attempt)
	if err == nil {
		if fmt.Sprint(existing["content_sha256"]) == digest && fmt.Sprint(existing["repo_path"]) == pathText {
			if kind == "escalation" {
				block, ok := frontMatterBlock(string(payload))
				if !ok {
					return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
				}
				frontMatter, err := parseFrontMatterBlock(block)
				if err != nil {
					return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
				}
				err = linkEscalationArtifact(ctx, runner, repositoryID, frontMatter, fmt.Sprint(existing["artifact_id"]), fmt.Sprint(job["run_id"]), jobID, sessionID, pathText, digest, now)
				if err != nil {
					return nil, err
				}
			}
			return map[string]any{"status": "already_published", "artifact_id": existing["artifact_id"], "placement": placement}, nil
		}
		return nil, rpc.NewError("artifact_error", artifactLogicalNameConflictMessage, nil)
	}
	if !errorsIsNoRows(err) {
		return nil, err
	}
	// RFC 0095 §7 / GH #58: the artifacts table also enforces
	// UNIQUE (repository_id, run_id, repo_path, content_sha256). The
	// logical-name pre-check above does not cover this constraint, so a
	// publish-artifact-then-submit-review flow that re-publishes the same
	// finding body under a different logical name (e.g. the default
	// "review" logical name on submit-review) used to crash the INSERT with
	// a raw pgx 23505 on
	// artifacts_repository_id_run_id_repo_path_content_sha256_key. Detect
	// the already-published tuple here and treat it as an idempotent no-op
	// success: reuse the existing artifact_id and emit a friendly structured
	// notice so the caller (review.submit) can still record the verdict.
	// Publishing *different* content at the same repo_path produces a
	// different content_sha256, so it falls through to a fresh INSERT and is
	// not silently collapsed; the same-content tuple is the only case we fold.
	// RFC 0095 §1 / #84: the path+content uniqueness key is now attempt-scoped
	// (UNIQUE (repository_id, run_id, repo_path, content_sha256, attempt)). Scope
	// the idempotent-no-op detection to the CURRENT attempt so a fresh attempt
	// that happens to republish byte-identical content at the same path still
	// INSERTs its own attempt-scoped row (rather than folding into the prior
	// attempt's row); within an attempt the same tuple remains an idempotent
	// no-op success as before (#58).
	pathDup, err := oneRow(ctx, runner, `
		SELECT artifact_id, logical_name FROM striatumd.artifacts
		 WHERE repository_id = $1 AND run_id = $2 AND repo_path = $3 AND content_sha256 = $4
		   AND attempt = $5
		 LIMIT 1`, repositoryID, job["run_id"], pathText, digest, attempt)
	if err == nil {
		if kind == "escalation" {
			block, ok := frontMatterBlock(string(payload))
			if !ok {
				return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
			}
			frontMatter, err := parseFrontMatterBlock(block)
			if err != nil {
				return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
			}
			if err := linkEscalationArtifact(ctx, runner, repositoryID, frontMatter, fmt.Sprint(pathDup["artifact_id"]), fmt.Sprint(job["run_id"]), jobID, sessionID, pathText, digest, now); err != nil {
				return nil, err
			}
		}
		return map[string]any{
			"status":         "already_published",
			"artifact_id":    pathDup["artifact_id"],
			"sha256":         digest,
			"placement":      placement,
			"notice":         "artifact with identical content already published at this path; reusing existing artifact_id (idempotent no-op)",
			"logical_name":   fmt.Sprint(pathDup["logical_name"]),
			"reused_logical": logicalName,
		}, nil
	}
	if !errorsIsNoRows(err) {
		return nil, err
	}
	artifactID, err := newID("art")
	if err != nil {
		return nil, err
	}
	authorLine := firstAuthorLine(payload)

	// RFC 0123: placement, not kind alone, decides whether the body uploads to
	// blob storage. Workflows without explicit placement still resolve through
	// the legacy RFC 0072 kind defaults.
	var blobKey, blobSha256, blobContentType string
	if packageBlobClient != nil && artifactcontracts.PlacementUsesBlob(placement) {
		bucket, err := lookupRepoBlobBucket(ctx, runner, repositoryID)
		if err != nil {
			return nil, err
		}
		if bucket != "" {
			runID := fmt.Sprint(job["run_id"])
			blobKey = blob.ArtifactKey(runID, jobID, logicalName)
			blobContentType = artifactContentType(pathText)
			uploadedSha, err := packageBlobClient.PutBytes(ctx, bucket, blobKey, payload, blobContentType)
			if err != nil {
				return nil, rpc.NewError("blob_publish_failed", err.Error(), map[string]any{
					"bucket": bucket,
					"key":    blobKey,
				})
			}
			if uploadedSha != digest {
				return nil, rpc.NewError("blob_publish_failed", "sha256 mismatch after upload", map[string]any{
					"bucket":   bucket,
					"key":      blobKey,
					"expected": digest,
					"got":      uploadedSha,
				})
			}
			blobSha256 = digest
		}
	}

	tx, ok := runner.(db.TxRunner)
	if !ok {
		return nil, fmt.Errorf("runner does not support transactional artifact append")
	}
	// RFC 0110 §7: at phase audit_artifacts this routes through the owner-owned
	// SECURITY DEFINER append_artifact_row (daemon-authority asserted in-DB);
	// before P1 it is the historical direct INSERT.
	if err := db.AppendArtifactInTx(ctx, tx, db.ArtifactRow{
		RepositoryID:    repositoryID,
		ArtifactID:      artifactID,
		RunID:           job["run_id"],
		JobID:           jobID,
		SessionID:       sessionID,
		LogicalName:     logicalName,
		ArtifactKind:    kind,
		RepoPath:        pathText,
		ContentSHA256:   digest,
		SizeBytes:       len(payload),
		CreatedAt:       now,
		AuthorLine:      nullable(authorLine),
		BlobKey:         nullable(blobKey),
		BlobSHA256:      nullable(blobSha256),
		BlobContentType: nullable(blobContentType),
		Attempt:         attempt,
		Placement:       placement,
	}); err != nil {
		return nil, err
	}
	if kind == "escalation" {
		block, ok := frontMatterBlock(string(payload))
		if !ok {
			return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
		}
		frontMatter, err := parseFrontMatterBlock(block)
		if err != nil {
			return nil, rpc.NewError("artifact_error", "escalation artifact front matter is required to link an escalation blocker", nil)
		}
		err = linkEscalationArtifact(ctx, runner, repositoryID, frontMatter, artifactID, fmt.Sprint(job["run_id"]), jobID, sessionID, pathText, digest, now)
		if err != nil {
			return nil, err
		}
	}
	eventPayload := map[string]any{
		"logical_name": logicalName,
		"path":         pathText,
		"sha256":       digest,
		"placement":    placement,
	}
	if blobKey != "" {
		eventPayload["blob_key"] = blobKey
	}
	if _, err := appendEvent(ctx, runner, repositoryID, job["run_id"], "artifact.published", sessionID, jobID, nil, artifactID, leaseID, eventPayload); err != nil {
		return nil, err
	}
	result := map[string]any{"status": "published", "artifact_id": artifactID, "sha256": digest, "placement": placement}
	if blobKey != "" {
		result["blob_key"] = blobKey
	}
	return result, nil
}

func resolvePublishPlacement(job map[string]any, kind, logicalName, pathText string, attempt int) string {
	for _, item := range resolveExpectedArtifactCycles(asList(job["expected_artifacts_json"]), attempt) {
		expected := asMap(item)
		if expected["logical_name"] == logicalName && expected["kind"] == kind && expected["path"] == pathText {
			return artifactcontracts.ResolvePlacement(kind, expected["placement"])
		}
	}
	return artifactcontracts.DefaultPlacementForKind(kind)
}

func artifactSourcePath(repoRoot, pathText string, activeWorktree map[string]any) (string, error) {
	sourceRoot := repoRoot
	if activeWorktree != nil {
		target, err := worktreeTarget(repoRoot, fmt.Sprint(activeWorktree["worktree_path"]))
		if err != nil {
			return "", err
		}
		sourceRoot = target
	}
	return repoRelativePath(sourceRoot, pathText, false)
}

// lookupRepoBlobBucket returns the per-repo bucket recorded at adopt
// time. Returns "" with no error when the repo's row has a NULL
// blob_bucket (operator has not enabled blob storage for this repo);
// callers then skip the upload and store the artifact body in the
// repo path only.
func lookupRepoBlobBucket(ctx context.Context, runner any, repositoryID string) (string, error) {
	row, err := oneRow(ctx, runner, `
		SELECT blob_bucket FROM striatumd.repositories
		 WHERE repository_id = $1 AND state != 'removed' LIMIT 1`, repositoryID)
	if err != nil {
		if errorsIsNoRows(err) {
			return "", nil
		}
		return "", err
	}
	bucket, _ := row["blob_bucket"].(string)
	return bucket, nil
}

// artifactContentType returns a conservative content type for the
// uploaded blob. Markdown files (the common case) get
// "text/markdown; charset=utf-8"; anything else gets
// "application/octet-stream" to avoid claiming a richer type than the
// runner actually verified.
func artifactContentType(pathText string) string {
	if strings.HasSuffix(strings.ToLower(pathText), ".md") {
		return "text/markdown; charset=utf-8"
	}
	if strings.HasSuffix(strings.ToLower(pathText), ".json") {
		return "application/json"
	}
	if strings.HasSuffix(strings.ToLower(pathText), ".txt") {
		return "text/plain; charset=utf-8"
	}
	return "application/octet-stream"
}

func pathAllowed(repoRoot, pathText string, writeScope map[string]any) bool {
	resolved, err := repoRelativePath(repoRoot, pathText, false)
	if err != nil {
		return false
	}
	forbidden := asList(writeScope["forbidden_paths"])
	if len(forbidden) == 0 {
		forbidden = []any{".striatum"}
	}
	for _, item := range forbidden {
		text, ok := item.(string)
		if !ok {
			continue
		}
		denied, err := repoRelativePath(repoRoot, text, true)
		if err != nil {
			continue
		}
		if sameOrInside(resolved, denied) {
			return false
		}
	}
	for _, item := range asList(writeScope["allowed_paths"]) {
		text, ok := item.(string)
		if !ok {
			continue
		}
		base, err := repoRelativePath(repoRoot, text, true)
		if err != nil {
			continue
		}
		if sameOrInside(resolved, base) {
			return true
		}
	}
	return false
}

func ValidateSandboxJail(repoRoot, pathText string) (string, error) {
	if filepath.IsAbs(pathText) {
		return "", fmt.Errorf("artifact path must be repo-relative")
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	repoRootResolved, err := filepath.EvalSymlinks(repoAbs)
	if err != nil {
		return "", fmt.Errorf("failed to resolve repo root: %w", err)
	}
	targetAbs := filepath.Clean(filepath.Join(repoRootResolved, pathText))
	targetResolved, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		if os.IsNotExist(err) {
			curr := targetAbs
			var rest []string
			for {
				parent := filepath.Dir(curr)
				if parent == curr {
					break
				}
				rest = append([]string{filepath.Base(curr)}, rest...)
				curr = parent

				resolvedParent, err := filepath.EvalSymlinks(curr)
				if err == nil {
					targetResolved = filepath.Join(append([]string{resolvedParent}, rest...)...)
					break
				}
				if !os.IsNotExist(err) {
					return "", err
				}
			}
			if targetResolved == "" {
				targetResolved = targetAbs
			}
		} else {
			return "", err
		}
	}

	if !sameOrInside(targetResolved, repoRootResolved) {
		return "", fmt.Errorf("artifact path must stay inside the repository: symlink_traversal_blocked")
	}
	return targetResolved, nil
}

func repoRelativePath(repoRoot, pathText string, allowState bool) (string, error) {
	resolved, err := ValidateSandboxJail(repoRoot, pathText)
	if err != nil {
		return "", err
	}
	repoAbs, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	repoRootResolved, err := filepath.EvalSymlinks(repoAbs)
	if err != nil {
		return "", err
	}
	statePath := filepath.Join(repoRootResolved, ".striatum")
	if !allowState && sameOrInside(resolved, statePath) {
		return "", fmt.Errorf("artifact path cannot be under .striatum")
	}
	return resolved, nil
}

func sameOrInside(path string, base string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."))
}

func ensureRequiredFrontMatter(kind string, path string, payload []byte) ([]byte, error) {
	updated, err := artifactcontracts.EnsureRequiredFrontMatter(kind, path, payload)
	if err != nil {
		return nil, err
	}
	if string(updated) != string(payload) {
		if err := os.WriteFile(path, updated, 0o644); err != nil {
			return nil, err
		}
	}
	return updated, nil
}

func validateArtifactFrontMatter(kind string, path string, payload []byte) error {
	if err := artifactcontracts.ValidateFrontMatter(kind, path, payload); err != nil {
		return rpc.NewError("artifact_error", err.Error(), nil)
	}
	return nil
}

func frontMatterBlock(text string) (string, bool) {
	return artifactcontracts.FrontMatterBlock(text)
}

func parseFrontMatterBlock(block string) (map[string]any, error) {
	return artifactcontracts.ParseFrontMatterBlock(block)
}

func validateMarkdownAuthorLine(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string, path string, payload []byte) error {
	if !isMarkdownPath(path) {
		return nil
	}
	text := string(payload)
	expected, err := expectedAuthorLine(ctx, runner, repositoryID, job, sessionID)
	if err != nil {
		return err
	}
	for _, line := range markdownTitleBlockAuthorLines(text) {
		canonical := canonicalBylineForm(line)
		if canonical == "" || canonical != expected {
			// #73/#106: name the target file and the exact replacement line so an
			// agent can repair in one edit without reverse-engineering the lane
			// attestation state that derived the byline. The same required byline
			// is also in the packet's expected_artifacts[].author_line.
			return rpc.NewError("artifact_error", fmt.Sprintf(
				"markdown artifact author line in %s is %q but the work packet requires %q; set the title-block author line to exactly %q and re-publish (this byline is also in the packet's expected_artifacts[].author_line).",
				path, canonical, expected, expected,
			), nil)
		}
	}
	return nil
}

func expectedAuthorLine(ctx context.Context, runner any, repositoryID string, job map[string]any, sessionID string) (string, error) {
	session, err := rowByID(ctx, runner, repositoryID, "sessions", "session_id", sessionID, false)
	if err != nil {
		return "", err
	}
	snapshotRun, err := rowByID(ctx, runner, repositoryID, "runs", "run_id", fmt.Sprint(job["run_id"]), false)
	if err != nil {
		return "", err
	}
	snapshot, err := rowByID(ctx, runner, repositoryID, "workflow_snapshots", "workflow_snapshot_id", fmt.Sprint(snapshotRun["workflow_snapshot_id"]), false)
	if err != nil {
		return "", err
	}
	workflow := asMap(snapshot["workflow_json"])
	laneID, _ := asMap(job["lane_selector_json"])["lane_id"].(string)
	attestation := sessionLaneAttestation(ctx, runner, repositoryID, sessionID)
	attested, _ := attestation["attested"].(bool)
	operatorLabel, _ := nullable(session["operator_label"]).(string)
	author := artifactAuthorIdentity(
		workflow,
		fmt.Sprint(job["role_id"]),
		laneID,
		fmt.Sprint(job["workflow_job_id"]),
		intValue(session["ordinal"]),
		attested,
		operatorLabel,
	)
	line, _ := author["line"].(string)
	if line == "" {
		return "", rpc.NewError("artifact_error", "expected artifact author line could not be derived", nil)
	}
	return strings.ToLower(line), nil
}

func isMarkdownPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown")
}

func firstAuthorLine(payload []byte) string {
	for _, line := range markdownTitleBlockAuthorLines(string(payload)) {
		if canonical := canonicalBylineForm(line); canonical != "" {
			return canonical
		}
	}
	return ""
}

func markdownTitleBlockAuthorLines(text string) []string {
	lines := strings.Split(text, "\n")
	frontMatter := []string{}
	titleBlock := []string{}
	bodyStart := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				bodyStart = i + 1
				break
			}
		}
		if bodyStart > 0 {
			for _, line := range lines[1 : bodyStart-1] {
				if canonicalBylineForm(line) != "" {
					frontMatter = append(frontMatter, line)
				}
			}
		}
	}
	limit := bodyStart + 40
	if limit > len(lines) {
		limit = len(lines)
	}
	for _, line := range lines[bodyStart:limit] {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if canonicalBylineForm(line) != "" {
			titleBlock = append(titleBlock, line)
			break
		}
	}
	if len(frontMatter) > 0 {
		return frontMatter
	}
	return titleBlock
}

func canonicalBylineForm(line string) string {
	stripped := strings.TrimSpace(line)
	for strings.HasPrefix(stripped, "#") {
		stripped = strings.TrimSpace(stripped[1:])
	}
	// Neutralize markdown emphasis around the byline. Asterisks never appear in
	// a valid author label, so deleting them all is safe. Underscores, however,
	// ARE legitimate inside labels (operator self-declared labels and role ids
	// like reviewer_ops_tests / findings_ledger), and emphasis only WRAPS the
	// line — so trim underscore wrappers at the ends rather than stripping
	// interior ones. Deleting interior underscores made an underscore label
	// permanently unmatchable against the packet's expected byline (#143).
	stripped = strings.ReplaceAll(stripped, "*", "")
	stripped = strings.Trim(stripped, "_")
	stripped = strings.TrimSpace(stripped)
	if !strings.HasPrefix(strings.ToLower(stripped), "author:") {
		return ""
	}
	value := strings.ToLower(strings.TrimSpace(strings.SplitN(stripped, ":", 2)[1]))
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if label, ok := legacyOperatorSelfDeclaredLabel(value); ok {
		return operatorAuthorLine(label)
	}
	return "author: " + value
}

func legacyOperatorSelfDeclaredLabel(value string) (string, bool) {
	const prefix = "operator [self-declared:"
	const suffix = "]"
	if !strings.HasPrefix(value, prefix) || !strings.HasSuffix(value, suffix) {
		return "", false
	}
	label := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, prefix), suffix))
	if label == "" {
		return "", false
	}
	return label, true
}

func errorsIsNoRows(err error) bool {
	return err == nil || err == pgx.ErrNoRows
}

func linkEscalationArtifact(
	ctx context.Context,
	runner any,
	repositoryID string,
	frontMatter map[string]any,
	artifactID string,
	runID string,
	jobID string,
	sessionID string,
	repoPath string,
	contentSha256 string,
	linkedAt string,
) error {
	escalationID, _ := frontMatter["escalation_id"].(string)
	if escalationID == "" {
		return rpc.NewError("artifact_error", "escalation artifact front matter missing escalation_id", nil)
	}
	frontRunID, _ := frontMatter["run_id"].(string)
	if frontRunID != runID {
		return rpc.NewError("artifact_error", "escalation artifact run_id must match publish context", nil)
	}
	if frontJobID, ok := frontMatter["job_id"].(string); ok && frontJobID != jobID {
		return rpc.NewError("artifact_error", "escalation artifact job_id must match publish context", nil)
	}
	if frontSessionID, ok := frontMatter["session_id"].(string); ok && frontSessionID != sessionID {
		return rpc.NewError("artifact_error", "escalation artifact session_id must match publish context", nil)
	}

	blocker, err := oneRow(ctx, runner, `
		SELECT blocker_id, run_id, severity, blocker_kind, payload_json
		  FROM striatumd.blockers
		 WHERE repository_id = $1
		   AND blocker_id = $2
		   FOR UPDATE`, repositoryID, escalationID)
	if err != nil {
		return rpc.NewError("artifact_error", "escalation artifact escalation_id does not match an existing blocker", nil)
	}
	if fmt.Sprint(blocker["run_id"]) != runID {
		return rpc.NewError("artifact_error", "escalation artifact blocker must belong to the same run", nil)
	}
	if !isEscalationClassBlocker(blocker) {
		return rpc.NewError("artifact_error", "escalation artifact blocker is not escalation-class", nil)
	}

	metadata := map[string]any{
		"artifact_id":    artifactID,
		"repo_path":      repoPath,
		"content_sha256": contentSha256,
		"linked_at":      linkedAt,
		"link_source":    "artifact.publish",
	}

	payloadJSONRaw := blocker["payload_json"]
	var payloadJSON map[string]any
	if payloadStr, ok := payloadJSONRaw.(string); ok {
		_ = json.Unmarshal([]byte(payloadStr), &payloadJSON)
	} else if payloadBytes, ok := payloadJSONRaw.([]byte); ok {
		_ = json.Unmarshal(payloadBytes, &payloadJSON)
	} else if payloadMap, ok := payloadJSONRaw.(map[string]any); ok {
		payloadJSON = payloadMap
	}
	if payloadJSON == nil {
		payloadJSON = map[string]any{}
	}

	if existingLink, ok := payloadJSON["escalation_artifact"].(map[string]any); ok {
		for _, key := range []string{"artifact_id", "repo_path", "content_sha256", "link_source"} {
			if existingLink[key] != metadata[key] {
				return rpc.NewError("artifact_error", "escalation blocker is already linked to a different artifact", nil)
			}
		}
		return nil
	}

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	exec, ok := runner.(interface {
		Exec(context.Context, string, ...any) error
	})
	if !ok {
		return fmt.Errorf("runner does not support exec")
	}

	if err := exec.Exec(ctx, `
		UPDATE striatumd.blockers
		   SET payload_json = jsonb_set(
				   COALESCE(payload_json, '{}'::jsonb),
				   '{escalation_artifact}',
				   $1,
				   true
			   )
		 WHERE repository_id = $2
		   AND blocker_id = $3`, string(metadataBytes), repositoryID, escalationID); err != nil {
		return err
	}

	if err := exec.Exec(ctx, `
		UPDATE striatumd.escalation_inbox
		   SET payload_json = jsonb_set(
				   COALESCE(payload_json, '{}'::jsonb),
				   '{escalation_artifact}',
				   $1,
				   true
			   )
		 WHERE repository_id = $2
		   AND escalation_id = $3`, string(metadataBytes), repositoryID, escalationID); err != nil {
		return err
	}

	return nil
}

func isEscalationClassBlocker(blocker map[string]any) bool {
	severity := fmt.Sprint(blocker["severity"])
	kind := fmt.Sprint(blocker["blocker_kind"])
	return severity == "human_checkpoint" ||
		kind == "ambiguous_goal" ||
		kind == "missing_authority" ||
		kind == "contradicting_decisions" ||
		kind == "no_available_reviewer_lane" ||
		kind == "committee_stalemate" ||
		kind == "override_required" ||
		kind == "ai_self_declared" ||
		// RFC 0101 Phase 4: daemon-authored recovery-exhausted escalation.
		kind == recoveryExhaustedBlockerKind ||
		// RFC 0118 P0-3: run-completion provenance gate escalation.
		kind == provenanceGateFailedBlockerKind
}
