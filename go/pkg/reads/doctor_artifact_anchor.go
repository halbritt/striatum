package reads

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/halbritt/striatum/go/pkg/artifactcontracts"
	"github.com/halbritt/striatum/go/pkg/db"
)

const (
	artifactAnchorHashMismatch   = "artifact_anchor_hash_mismatch"
	artifactAnchorMissingFile    = "artifact_anchor_missing_file"
	artifactBlobMetadataMissing  = "artifact_blob_metadata_missing"
	artifactBlobBodyVerifyFailed = "artifact_blob_body_verify_failed"

	// Legibility warning codes (D-doctor-integrity-legibility): reclassified,
	// non-ok-reddening counterparts of the integrity problems above.
	artifactLegacyUnverifiable = "artifact_legacy_unverifiable"
	artifactDebrisTerminalRun  = "artifact_debris_terminal_run"

	// P1 legibility warning codes (D205): a deliverable whose path is still live
	// on the default-branch tip (only the recorded draft sha is unverifiable) and
	// a curated, operator-acknowledged immaterial loss.
	artifactSupersededOnDefaultBranch = "artifact_superseded_on_default_branch"
	artifactAcknowledgedLoss          = "artifact_acknowledged_loss"

	// defaultRefHistoryRevisionCap bounds the default-branch history scan (Rule A)
	// so a pathological history can never blow up or hang a doctor pass.
	defaultRefHistoryRevisionCap = 200
)

// artifactAnchorPass bundles the per-doctor-pass caches threaded through the
// artifact integrity checks so a pass that scans hundreds of rows does bounded
// work: the default-branch ref is resolved once per repo root, default-branch
// history hits are memoized by (root|ref|path|sha), and the curated
// acknowledged-loss baseline is loaded once per repo root.
type artifactAnchorPass struct {
	defaultRefByRoot      map[string]string
	localDefaultRefByRoot map[string]string
	historyByKey          map[string]bool
	ackByRoot             map[string]acknowledgedLossSet
}

func newArtifactAnchorPass() *artifactAnchorPass {
	return &artifactAnchorPass{
		defaultRefByRoot:      map[string]string{},
		localDefaultRefByRoot: map[string]string{},
		historyByKey:          map[string]bool{},
		ackByRoot:             map[string]acknowledgedLossSet{},
	}
}

// defaultRefs returns the default-branch refs the anchor checks should consult
// for a repo root: the primary (remote-preferred) ref from
// resolveDefaultRefCached, plus — when it differs — the LOCAL default branch
// (refs/heads/main) that `run integrate` advances. Probing both lets a
// locally-integrated-but-unpushed deliverable clear (#504) instead of staying an
// ok-reddening hash mismatch until the run is pushed to origin. Both lookups are
// memoized per repo root; empties and duplicates are dropped.
func (p *artifactAnchorPass) defaultRefs(ctx context.Context, repoRoot, primary string) []string {
	refs := []string{}
	if strings.TrimSpace(primary) != "" {
		refs = appendUniqueString(refs, primary)
	}
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return refs
	}
	local, ok := p.localDefaultRefByRoot[repoRoot]
	if !ok {
		local = readGitLocalDefaultBranchRef(ctx, repoRoot)
		p.localDefaultRefByRoot[repoRoot] = local
	}
	if strings.TrimSpace(local) != "" {
		refs = appendUniqueString(refs, local)
	}
	return refs
}

// ackSet loads (and memoizes) the curated acknowledged-loss baseline for a repo
// root. Loading never errors; a missing/unparseable file yields an empty set so
// nothing is downgraded.
func (p *artifactAnchorPass) ackSet(repoRoot string) acknowledgedLossSet {
	repoRoot = strings.TrimSpace(repoRoot)
	if set, ok := p.ackByRoot[repoRoot]; ok {
		return set
	}
	set := loadAcknowledgedLossSet(repoRoot)
	p.ackByRoot[repoRoot] = set
	return set
}

func doctorArtifactAnchorIntegrity(ctx context.Context, runner db.Runner, repositoryID string, blobBlock map[string]any) (map[string]any, []string, []map[string]any, []string, []map[string]any) {
	block := map[string]any{
		"checked": false,
		"skipped": artifactAnchorSkipReason(repositoryID, blobBlock),
	}
	if block["skipped"] != "" {
		return block, nil, nil, nil, nil
	}

	rows, err := collectRows(ctx, runner, `
		SELECT a.repository_id, a.artifact_id, a.run_id, a.job_id, a.logical_name, a.repo_path,
		       a.content_sha256, a.artifact_kind, a.blob_key,
		       a.blob_sha256, a.blob_content_type`+artifactPlacementProjectionAny(ctx, runner, "a")+`,
		       j.workflow_job_id, j.attempt, j.write_scope_json,
		       r.repo_root, r.branch_name, r.state AS run_state
		  FROM striatumd.artifacts a
		  JOIN striatumd.jobs j
		    ON j.repository_id = a.repository_id
		   AND j.job_id = a.job_id
		  JOIN striatumd.runs r
		    ON r.repository_id = a.repository_id
		   AND r.run_id = a.run_id
		 WHERE a.repository_id = $1
		   AND j.state = 'completed'
		   AND COALESCE(j.write_scope_json->>'repo_write', 'false') = 'true'
		 ORDER BY a.run_id, a.job_id, a.created_at, a.artifact_id`,
		repositoryID,
	)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil, nil, nil
	}

	block["checked"] = true
	block["skipped"] = nil
	block["artifact_count"] = len(rows)
	decorateArtifactPlacements(rows)
	// #303: suppress artifacts an operator has tombstoned via `recovery
	// prune-debris`. The prune verb never deletes the append-only artifact row
	// (owner-owned, delete/update triggers); it records an append-only
	// recovery.debris_pruned event keyed on the artifact_id. The doctor pass
	// honors those tombstones so a pruned terminal-run debris artifact stops
	// reddening/degrading the report — one set query, then a per-row skip.
	tombstoned, err := debrisPrunedArtifactIDs(ctx, runner, repositoryID)
	if err != nil {
		block["error"] = err.Error()
		return block, nil, nil, nil, nil
	}
	block["debris_pruned_count"] = len(tombstoned)
	problems := []string{}
	records := []map[string]any{}
	warnings := []string{}
	warningRecords := []map[string]any{}
	gitChecked := 0
	blobChecked := 0
	bucket := ""
	if packageBlobClient != nil {
		var err error
		bucket, err = lookupRepoBlobBucketRead(ctx, runner, repositoryID)
		if err != nil {
			block["error"] = err.Error()
			return block, nil, nil, nil, nil
		}
	}
	pass := newArtifactAnchorPass()
	for _, row := range rows {
		// #303: a tombstoned (operator-pruned) artifact is recorded debris; skip
		// it entirely so it neither reds nor degrades the report.
		if tombstoned[stringFrom(row, "artifact_id")] {
			continue
		}
		placement := artifactcontracts.ResolvePlacement(stringFrom(row, "artifact_kind"), row["placement"])
		row["placement"] = placement
		defaultRef := resolveDefaultRefCached(ctx, strings.TrimSpace(stringFrom(row, "repo_root")), pass.defaultRefByRoot)
		var problem string
		var record map[string]any
		var warning string
		var warningRecord map[string]any
		if artifactcontracts.PlacementUsesBlob(placement) {
			blobChecked++
			problem, record, warning, warningRecord = checkBlobExhaustArtifact(ctx, row, bucket, defaultRef, pass)
		} else if artifactcontracts.PlacementUsesGitAnchor(placement) {
			gitChecked++
			problem, record, warning, warningRecord = checkArtifactAnchor(ctx, row, defaultRef, pass)
		}
		if problem != "" {
			problems = append(problems, problem)
			records = append(records, record)
		}
		if warning != "" {
			warnings = append(warnings, warning)
			warningRecords = append(warningRecords, warningRecord)
		}
	}
	block["git_anchor_count"] = gitChecked
	block["blob_exhaust_count"] = blobChecked
	block["problem_count"] = len(problems)
	block["warning_count"] = len(warnings)
	// Surface the curated acknowledged-loss baseline status (Rule C) so verbose
	// consumers can see whether it was absent (the normal state), loaded, or
	// unparseable. All rows of a pass share a repository (one repo root), so the
	// first row's repo root is representative; the load is memoized.
	ackRepoRoot := ""
	if len(rows) > 0 {
		ackRepoRoot = strings.TrimSpace(stringFrom(rows[0], "repo_root"))
	}
	block["acknowledged_loss_status"] = pass.ackSet(ackRepoRoot).status
	return block, problems, records, warnings, warningRecords
}

func checkBlobExhaustArtifact(ctx context.Context, row map[string]any, bucket, defaultRef string, pass *artifactAnchorPass) (string, map[string]any, string, map[string]any) {
	blobKey := strings.TrimSpace(stringFrom(row, "blob_key"))
	expected := strings.TrimSpace(stringFrom(row, "blob_sha256"))
	if expected == "" {
		expected = strings.TrimSpace(stringFrom(row, "content_sha256"))
	}
	if blobKey == "" {
		// Legibility rule 3 (D-doctor-integrity-legibility): an empty blob_key is
		// the signature of a legacy artifact that predates RFC 0125 blob storage,
		// not a fresh durability gap. If its content is still verifiable on a
		// durable ref, the default-branch tip, or (D205 Rule A) anywhere in
		// default-branch history, downgrade to a legacy warning; only genuine loss
		// (content absent everywhere) stays an ok-reddening problem.
		if artifactContentPreserved(ctx, row, defaultRef, pass) {
			warning, record := artifactWarning(artifactLegacyUnverifiable, row, "blob_key_absent_predates_blob_storage", defaultRef)
			return "", nil, warning, record
		}
		if terminalDebrisRunState(stringFrom(row, "run_state")) {
			warning, record := artifactWarning(artifactDebrisTerminalRun, row, "blob_key_or_sha_missing", "")
			return "", nil, warning, record
		}
		repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
		repoPath, pathOK := cleanArtifactAnchorPath(stringFrom(row, "repo_path"))
		// D205 Rule B (+#504): the deliverable path is still live on a default-branch
		// tip — primary (remote) OR the local default branch `run integrate` advances
		// — and only the recorded draft sha is unverifiable -> superseded warning.
		if pathOK {
			for _, ref := range pass.defaultRefs(ctx, repoRoot, defaultRef) {
				if pathExistsOnRef(ctx, repoRoot, ref, repoPath) {
					warning, record := artifactWarning(artifactSupersededOnDefaultBranch, row, "recorded_content_sha_unverifiable_path_live_on_default", ref)
					return "", nil, warning, record
				}
			}
		}
		// D205 Rule C: a curated, sha-bound acknowledged immaterial loss -> warning.
		if entry, ok := pass.ackSet(repoRoot).honor(stringFrom(row, "artifact_id"), expected); ok {
			warning, record := acknowledgedLossWarning(row, entry)
			return "", nil, warning, record
		}
		problem, record := artifactBlobProblem(artifactBlobMetadataMissing, row, "blob_key_or_sha_missing")
		return problem, record, "", nil
	}
	if expected == "" {
		problem, record := artifactBlobProblem(artifactBlobMetadataMissing, row, "blob_key_or_sha_missing")
		return problem, record, "", nil
	}
	if packageBlobClient == nil {
		return "", nil, "", nil
	}
	if bucket == "" {
		return blobBodyVerifyResult(row, "repository_blob_bucket_missing")
	}
	if _, err := packageBlobClient.GetBytes(ctx, bucket, blobKey, expected); err != nil {
		return blobBodyVerifyResult(row, err.Error())
	}
	return "", nil, "", nil
}

// blobBodyVerifyResult classifies a blob-body verification failure. A failure on
// an abandoned (terminal-debris) run is leftover, not an active gap, so it is a
// warning; otherwise it stays an ok-reddening problem.
func blobBodyVerifyResult(row map[string]any, detail string) (string, map[string]any, string, map[string]any) {
	if terminalDebrisRunState(stringFrom(row, "run_state")) {
		warning, record := artifactWarning(artifactDebrisTerminalRun, row, detail, "")
		return "", nil, warning, record
	}
	problem, record := artifactBlobProblem(artifactBlobBodyVerifyFailed, row, detail)
	return problem, record, "", nil
}

func artifactAnchorSkipReason(repositoryID string, blobBlock map[string]any) string {
	if strings.TrimSpace(repositoryID) == "" {
		return "repository_id_missing"
	}
	if blobBlock["configured"] != true {
		return "blob_not_configured"
	}
	if blobBlock["reachable"] != true {
		return "blob_unreachable"
	}
	if stringFrom(blobBlock, "bucket_status") != "ok" {
		return "blob_bucket_not_ok"
	}
	return ""
}

func checkArtifactAnchor(ctx context.Context, row map[string]any, defaultRef string, pass *artifactAnchorPass) (string, map[string]any, string, map[string]any) {
	repoPath, ok := cleanArtifactAnchorPath(stringFrom(row, "repo_path"))
	if !ok {
		problem, record := artifactAnchorProblem(artifactAnchorMissingFile, row, "", "", "invalid_repo_path")
		return problem, record, "", nil
	}
	expected := strings.TrimSpace(stringFrom(row, "content_sha256"))
	repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
	refs := durableWorktreeProbeRefs(ctx, repoRoot, row)
	if repoRoot == "" || expected == "" || len(refs) == 0 {
		return "", nil, "", nil
	}

	checkedRefs := []string{}
	fileFound := false
	var mismatchAnchor artifactAnchorProbe
	var firstAnchor artifactAnchorProbe
	for _, ref := range refs {
		commit, err := readGitCommit(ctx, repoRoot, ref)
		if err != nil {
			continue
		}
		checkedRefs = appendUniqueString(checkedRefs, ref)
		if firstAnchor.Ref == "" {
			firstAnchor = artifactAnchorProbe{Ref: ref, Commit: commit}
		}
		probe, err := readGitBlobSHA256(ctx, repoRoot, commit, repoPath)
		if err != nil {
			problem, record := artifactAnchorProblem(artifactAnchorMissingFile, row, ref, commit, err.Error())
			return problem, record, "", nil
		}
		if !probe.Exists {
			continue
		}
		fileFound = true
		if probe.SHA256 == expected {
			return "", nil, "", nil
		}
		if mismatchAnchor.Ref == "" {
			mismatchAnchor = artifactAnchorProbe{Ref: ref, Commit: commit, SHA256: probe.SHA256}
		}
	}
	row["checked_refs"] = checkedRefs

	// #504: consult BOTH the primary (remote-preferred) default ref AND the local
	// default branch that `run integrate` advances, so a locally-integrated but
	// not-yet-pushed deliverable clears the same way a pushed one does instead of
	// staying an ok-reddening hash mismatch until the run reaches origin.
	defaultRefList := pass.defaultRefs(ctx, repoRoot, defaultRef)

	// Legibility rule 1 (D-doctor-integrity-legibility): content that matches the
	// artifact's content_sha256 at a default-branch tip is durably preserved
	// (the run branch was merged then deleted). That is not loss, so it is clean —
	// not even a warning, since there is nothing for the operator to anchor.
	for _, ref := range defaultRefList {
		if artifactContentMatchesRef(ctx, repoRoot, ref, repoPath, expected) {
			return "", nil, "", nil
		}
	}

	// D205 Rule A: content that matches the artifact's content_sha256 at ANY
	// reachable revision of a default branch (not only its tip) is durably
	// preserved — the deliverable was merged, then the path was later deleted or
	// rewritten. The recorded content still has a durable home in history, so this
	// is clean, like a tip match. Must run before the hash_mismatch verdict so a
	// merged-then-edited path is recovered rather than flagged.
	for _, ref := range defaultRefList {
		if artifactContentInDefaultRefHistory(ctx, repoRoot, ref, repoPath, expected, pass.historyByKey) {
			return "", nil, "", nil
		}
	}

	// Legibility rule 2: a finding from an abandoned (terminal-debris) run is
	// archived leftover, not an active gap.
	if terminalDebrisRunState(stringFrom(row, "run_state")) {
		detail := "path_not_present_in_checked_anchors"
		if fileFound {
			detail = "anchor_content_sha_mismatch"
		}
		warning, record := artifactWarning(artifactDebrisTerminalRun, row, detail, "")
		return "", nil, warning, record
	}

	// D205 Rule B: the deliverable's repo_path is still live on a default-branch
	// tip (in any content). The deliverable landed at that path; only the recorded
	// draft content_sha256 is unverifiable (the lane draft was revised before
	// merge). That is not an active durability gap -> warning, not a problem.
	for _, ref := range defaultRefList {
		if pathExistsOnRef(ctx, repoRoot, ref, repoPath) {
			warning, record := artifactWarning(artifactSupersededOnDefaultBranch, row, "recorded_draft_sha_unverifiable_path_live_on_default", ref)
			return "", nil, warning, record
		}
	}

	// D205 Rule C: a genuinely lost artifact (path absent from the default branch,
	// content on no ref) whose loss the operator has reviewed and recorded in the
	// curated, sha-bound acknowledged-loss baseline -> warning. Anything NOT in the
	// baseline (and any id-match-but-sha-mismatch) still reds ok below.
	if entry, ok := pass.ackSet(repoRoot).honor(stringFrom(row, "artifact_id"), expected); ok {
		warning, record := acknowledgedLossWarning(row, entry)
		return "", nil, warning, record
	}

	if fileFound {
		problem, record := artifactAnchorProblem(artifactAnchorHashMismatch, row, mismatchAnchor.Ref, mismatchAnchor.Commit, mismatchAnchor.SHA256)
		return problem, record, "", nil
	}
	problem, record := artifactAnchorProblem(artifactAnchorMissingFile, row, firstAnchor.Ref, firstAnchor.Commit, "path_not_present_in_checked_anchors")
	return problem, record, "", nil
}

// artifactContentPreserved reports whether the artifact's content_sha256 is
// present at its repo_path on any durable ref (run branch / refs/striatum pins),
// at the default-branch tip, or (D205 Rule A) at any reachable revision of the
// default branch's history. It degrades safely: a missing repo root, path, sha,
// or ref is treated as "not preserved here".
func artifactContentPreserved(ctx context.Context, row map[string]any, defaultRef string, pass *artifactAnchorPass) bool {
	repoRoot := strings.TrimSpace(stringFrom(row, "repo_root"))
	repoPath, ok := cleanArtifactAnchorPath(stringFrom(row, "repo_path"))
	expected := strings.TrimSpace(stringFrom(row, "content_sha256"))
	if repoRoot == "" || !ok || expected == "" {
		return false
	}
	// #504: probe both default refs (remote-preferred primary + the local default
	// branch `run integrate` advances) so a locally-integrated legacy artifact is
	// recognized as preserved before the run is pushed to origin.
	defaultRefList := pass.defaultRefs(ctx, repoRoot, defaultRef)
	refs := durableWorktreeProbeRefs(ctx, repoRoot, row)
	for _, ref := range defaultRefList {
		refs = appendUniqueString(refs, ref)
	}
	if artifactContentMatchesAnyRef(ctx, repoRoot, repoPath, expected, refs) {
		return true
	}
	// Rule A: also consult default-branch history, not only ref tips, so a
	// legacy artifact merged then deleted/rewritten is still recognized as
	// preserved (legacy warning) rather than reported as missing metadata.
	for _, ref := range defaultRefList {
		if artifactContentInDefaultRefHistory(ctx, repoRoot, ref, repoPath, expected, pass.historyByKey) {
			return true
		}
	}
	return false
}

// artifactContentInDefaultRefHistory reports whether expectedSHA equals the
// sha256 of repoPath's blob at ANY reachable revision of the default branch, not
// only its tip (D205 Rule A). This recovers content that was merged to the
// default branch and then deleted or rewritten later: the recorded draft still
// has a durable home in history. It is bounded (at most
// defaultRefHistoryRevisionCap revisions of that path), ctx-cancellable, memoized
// per (root|ref|path|sha), and safe-degrades to false on any git error.
func artifactContentInDefaultRefHistory(ctx context.Context, repoRoot, defaultRef, repoPath, expectedSHA string, cache map[string]bool) bool {
	repoRoot = strings.TrimSpace(repoRoot)
	defaultRef = strings.TrimSpace(defaultRef)
	repoPath = strings.TrimSpace(repoPath)
	expectedSHA = strings.TrimSpace(expectedSHA)
	if repoRoot == "" || defaultRef == "" || repoPath == "" || expectedSHA == "" {
		return false
	}
	key := repoRoot + "|" + defaultRef + "|" + repoPath + "|" + expectedSHA
	if cache != nil {
		if hit, ok := cache[key]; ok {
			return hit
		}
	}
	result := computeContentInDefaultRefHistory(ctx, repoRoot, defaultRef, repoPath, expectedSHA)
	if cache != nil {
		cache[key] = result
	}
	return result
}

func computeContentInDefaultRefHistory(ctx context.Context, repoRoot, defaultRef, repoPath, expectedSHA string) bool {
	// `git log <defaultRef> --max-count=N --format=%H -- <repoPath>` lists only the
	// commits that touched that path (cheap; bounded by --max-count so a
	// pathological history cannot blow up the pass). A missing ref/path errors out
	// and we safe-degrade to false.
	out, err := readGitOutput(ctx, repoRoot, "log", "--max-count="+strconv.Itoa(defaultRefHistoryRevisionCap), "--format=%H", defaultRef, "--", repoPath)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		if ctx.Err() != nil {
			return false
		}
		commit := strings.TrimSpace(line)
		if commit == "" {
			continue
		}
		probe, err := readGitBlobSHA256(ctx, repoRoot, commit, repoPath)
		if err != nil || !probe.Exists {
			continue
		}
		if probe.SHA256 == expectedSHA {
			return true
		}
	}
	return false
}

// pathExistsOnRef reports whether repoPath exists in the tree at ref's tip (D205
// Rule B). ctx-cancellable; safe-degrades to false on a missing ref/path or any
// git error.
func pathExistsOnRef(ctx context.Context, repoRoot, ref, repoPath string) bool {
	repoRoot = strings.TrimSpace(repoRoot)
	ref = strings.TrimSpace(ref)
	repoPath = strings.TrimSpace(repoPath)
	if repoRoot == "" || ref == "" || repoPath == "" {
		return false
	}
	commit, err := readGitCommit(ctx, repoRoot, ref)
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "cat-file", "-e", commit+":"+repoPath)
	return cmd.Run() == nil
}

func artifactContentMatchesAnyRef(ctx context.Context, repoRoot, repoPath, expectedSHA string, refs []string) bool {
	for _, ref := range refs {
		if artifactContentMatchesRef(ctx, repoRoot, ref, repoPath, expectedSHA) {
			return true
		}
	}
	return false
}

func artifactContentMatchesRef(ctx context.Context, repoRoot, ref, repoPath, expectedSHA string) bool {
	commit, err := readGitCommit(ctx, repoRoot, ref)
	if err != nil {
		return false
	}
	probe, err := readGitBlobSHA256(ctx, repoRoot, commit, repoPath)
	if err != nil || !probe.Exists {
		return false
	}
	return probe.SHA256 == expectedSHA
}

type artifactAnchorProbe struct {
	Ref    string
	Commit string
	SHA256 string
	Exists bool
}

func readGitBlobSHA256(ctx context.Context, repoRoot, commit, repoPath string) (artifactAnchorProbe, error) {
	body, present, err := readGitFileBytes(ctx, repoRoot, commit, repoPath)
	if err != nil {
		return artifactAnchorProbe{}, err
	}
	if !present {
		return artifactAnchorProbe{Commit: commit, Exists: false}, nil
	}
	sum := sha256.Sum256(body)
	return artifactAnchorProbe{Commit: commit, SHA256: hex.EncodeToString(sum[:]), Exists: true}, nil
}

// readGitFileBytes returns the raw bytes of repoPath at commit (git show
// commit:repoPath, stdout only so the body is byte-exact for sha verification).
// present=false means the path is absent in that commit/tree.
func readGitFileBytes(ctx context.Context, repoRoot, commit, repoPath string) ([]byte, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "show", commit+":"+repoPath)
	body, err := cmd.Output()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, false, nil
		}
		return nil, false, err
	}
	return body, true, nil
}

func cleanArtifactAnchorPath(pathText string) (string, bool) {
	pathText = strings.TrimSpace(filepath.ToSlash(pathText))
	if pathText == "" || strings.HasPrefix(pathText, "/") {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(pathText))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func artifactAnchorProblem(check string, row map[string]any, anchorRef, anchorCommit, detail string) (string, map[string]any) {
	artifactID := stringFrom(row, "artifact_id")
	repoPath := stringFrom(row, "repo_path")
	message := fmt.Sprintf("%s.%s: artifact %s at %s does not match its durable git anchor", check, artifactID, artifactID, repoPath)
	if check == artifactAnchorMissingFile {
		message = fmt.Sprintf("%s.%s: artifact %s missing at %s in durable git anchor", check, artifactID, artifactID, repoPath)
	}
	contextMap := map[string]any{
		"repository_id":  row["repository_id"],
		"run_id":         row["run_id"],
		"job_id":         row["job_id"],
		"artifact_id":    row["artifact_id"],
		"logical_name":   row["logical_name"],
		"repo_path":      row["repo_path"],
		"content_sha256": row["content_sha256"],
		"placement":      row["placement"],
		"anchor_kind":    artifactAnchorKind(anchorRef),
		"anchor_ref":     nullableString(anchorRef),
		"anchor_commit":  nullableString(anchorCommit),
		"checked_refs":   row["checked_refs"],
	}
	if check == artifactAnchorHashMismatch {
		contextMap["anchor_content_sha256"] = detail
	} else {
		contextMap["reason"] = detail
	}
	record := map[string]any{
		"check":   check,
		"id":      artifactID,
		"context": contextMap,
	}
	return message, record
}

func artifactBlobProblem(check string, row map[string]any, detail string) (string, map[string]any) {
	artifactID := stringFrom(row, "artifact_id")
	message := fmt.Sprintf("%s.%s: blob-exhaust artifact %s does not have a verified blob body", check, artifactID, artifactID)
	if check == artifactBlobMetadataMissing {
		message = fmt.Sprintf("%s.%s: blob-exhaust artifact %s is missing blob metadata", check, artifactID, artifactID)
	}
	record := map[string]any{
		"check": check,
		"id":    artifactID,
		"context": map[string]any{
			"repository_id":     row["repository_id"],
			"run_id":            row["run_id"],
			"job_id":            row["job_id"],
			"artifact_id":       row["artifact_id"],
			"logical_name":      row["logical_name"],
			"repo_path":         row["repo_path"],
			"content_sha256":    row["content_sha256"],
			"placement":         row["placement"],
			"blob_key":          row["blob_key"],
			"blob_sha256":       row["blob_sha256"],
			"blob_content_type": row["blob_content_type"],
			"reason":            detail,
		},
	}
	return message, record
}

// artifactWarning builds the message + verbose record for a reclassified
// (warning, not problem) artifact finding. Records stay parallel to
// artifactBlobProblem / artifactAnchorProblem so verbose consumers can read both
// channels with the same shape.
func artifactWarning(check string, row map[string]any, detail, preservedRef string) (string, map[string]any) {
	artifactID := stringFrom(row, "artifact_id")
	repoPath := stringFrom(row, "repo_path")
	var message string
	switch check {
	case artifactLegacyUnverifiable:
		message = fmt.Sprintf("%s.%s: legacy artifact %s at %s predates blob storage; content is preserved on a durable ref or the default branch", check, artifactID, artifactID, repoPath)
	case artifactDebrisTerminalRun:
		message = fmt.Sprintf("%s.%s: artifact %s at %s belongs to an abandoned run; archived debris, not an active durability gap", check, artifactID, artifactID, repoPath)
	case artifactSupersededOnDefaultBranch:
		message = fmt.Sprintf("%s.%s: deliverable for artifact %s is present at %s on the default branch in a revised form; the recorded draft content_sha256 is not verifiable but this is not a durability gap", check, artifactID, artifactID, repoPath)
	case artifactAcknowledgedLoss:
		message = fmt.Sprintf("%s.%s: artifact %s at %s is a curated, operator-acknowledged immaterial loss; recorded as reviewed provenance, not an active durability gap", check, artifactID, artifactID, repoPath)
	default:
		message = fmt.Sprintf("%s.%s: artifact %s at %s reclassified to warning", check, artifactID, artifactID, repoPath)
	}
	contextMap := map[string]any{
		"repository_id":  row["repository_id"],
		"run_id":         row["run_id"],
		"job_id":         row["job_id"],
		"artifact_id":    row["artifact_id"],
		"logical_name":   row["logical_name"],
		"repo_path":      row["repo_path"],
		"content_sha256": row["content_sha256"],
		"placement":      row["placement"],
		"blob_key":       row["blob_key"],
		"run_state":      row["run_state"],
		"reason":         detail,
	}
	if preservedRef != "" {
		contextMap["preserved_ref"] = preservedRef
	}
	record := map[string]any{
		"check":   check,
		"id":      artifactID,
		"context": contextMap,
	}
	return message, record
}

// acknowledgedLossWarning builds the artifact_acknowledged_loss warning for a
// curated, sha-bound loss acknowledgment (D205 Rule C), carrying the operator's
// recorded reason + acknowledged_by into the verbose record context so the
// warning channel still tells the full provenance story.
func acknowledgedLossWarning(row map[string]any, entry acknowledgedLossEntry) (string, map[string]any) {
	warning, record := artifactWarning(artifactAcknowledgedLoss, row, entry.Reason, "")
	if contextMap, ok := record["context"].(map[string]any); ok {
		contextMap["acknowledged_by"] = entry.AcknowledgedBy
		if at := strings.TrimSpace(entry.AcknowledgedAt); at != "" {
			contextMap["acknowledged_at"] = at
		}
	}
	return warning, record
}

// DebrisPrunedEventType is the append-only event the `recovery prune-debris`
// verb records to tombstone a terminal-run debris artifact (#303). It is keyed
// on the event's artifact_id column (FK to striatumd.artifacts) so the doctor
// pass can suppress tombstoned artifacts with a single set query. The artifact
// row itself is never deleted — striatumd.artifacts is owner-owned and
// append-only — so the tombstone IS the prune.
const DebrisPrunedEventType = "recovery.debris_pruned"

// debrisPrunedArtifactIDs loads the set of artifact_ids the operator has
// tombstoned via recovery.debris_pruned events for the repository. One query,
// returned as a set the per-row loop checks. A missing/empty result yields an
// empty set so nothing is suppressed.
func debrisPrunedArtifactIDs(ctx context.Context, runner db.Runner, repositoryID string) (map[string]bool, error) {
	rows, err := collectRows(ctx, runner, `
		SELECT DISTINCT artifact_id
		  FROM striatumd.events
		 WHERE repository_id = $1
		   AND event_type = $2
		   AND artifact_id IS NOT NULL`,
		repositoryID, DebrisPrunedEventType,
	)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id := stringFrom(row, "artifact_id"); id != "" {
			set[id] = true
		}
	}
	return set, nil
}

func artifactAnchorKind(ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return worktreeAnchorRunBranch
	}
	return worktreeAnchorJobPin
}
