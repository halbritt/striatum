package mutations

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// RFC 0125 daemon-as-porter (#278 + #281): when a repo-write lane published a
// git_publication artifact whose declared path the target repo ignores
// (.gitignore `build/`) AND the per-job worktree is detached, the lane cannot
// make the body durable itself (git add skips ignored paths; git.commit_apply
// refuses detached HEAD). ensurePerJobPublishedArtifactsDurable must now succeed
// because the daemon force-adds + commits the body the lane wrote, on the
// detached worktree HEAD.
func TestPorterCommitsGitignoredArtifactOnDetachedWorktree(t *testing.T) {
	if !haveGit(t) {
		return
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "porter_ignored", true)

	// The target repo ignores build/ — write the ignore rule into the (detached)
	// worktree working tree so a plain `git add` would skip the artifact.
	if err := os.WriteFile(filepath.Join(ids.worktreeRoot, ".gitignore"), []byte("build/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The lane wrote its handoff into the worktree but could not commit it.
	payload := []byte("S11 build handoff: pushed SHA 80dc82e7")
	repoPath := "build/HANDOFF.md"
	abs := filepath.Join(ids.worktreeRoot, filepath.FromSlash(repoPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_porter_ignored", "handoff", repoPath, payload, nil)

	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, false)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}

	// The daemon-porter makes the body durable; the durability gate passes.
	if err := ensurePublishedArtifactsDurableWithPorter(ctx, runner, ids.repoID, job, "work.complete"); err != nil {
		t.Fatalf("ensurePublishedArtifactsDurableWithPorter should pass after the porter commit, got: %v", err)
	}

	// The body is now reachable from the worktree HEAD (the porter force-committed
	// it past the ignore rule, on the detached HEAD).
	got := gitRun(t, ids.worktreeRoot, "show", "HEAD:"+repoPath)
	if strings.TrimSpace(got) != strings.TrimSpace(string(payload)) {
		t.Fatalf("HEAD:%s = %q, want %q", repoPath, got, payload)
	}
}

// RFC 0125 daemon-as-porter boundary (#272): when the lane could not even write
// the body into the operator-owned worktree, no file exists to commit, so the
// porter cannot reconstruct it and the durability gate must still refuse (the
// operator resolves it out of band, then recovery.reseal). The porter must never
// falsely report durability for a body that is not actually present.
func TestPorterLeavesResidualWhenBodyAbsent(t *testing.T) {
	if !haveGit(t) {
		return
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "porter_absent", true)

	// An artifact row is published, but the body file was never written into the
	// worktree (the lane OS user could not enter it).
	payload := []byte("body the lane could not place in the worktree")
	seedPublishedArtifact(t, ctx, runner, ids, "art_porter_absent", "handoff", "docs/HANDOFF.md", payload, nil)

	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, false)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}

	if err := ensurePublishedArtifactsDurableWithPorter(ctx, runner, ids.repoID, job, "work.complete"); err == nil {
		t.Fatalf("expected a durability refusal when the body is absent from the worktree; porter must not falsely pass")
	}
}

// #551 regression: a repo-write review / phase_synthesis job (e.g. a
// falsification_gate or verification_gate adjudicate) that published a
// git_publication artifact (a collaboration_ledger) via artifact.publish MUST have
// that body porter-committed AND anchored to the RUN BRANCH when it completes via
// review.verdict (completeReviewJob), exactly as the work.complete path does.
// Before the fix the body was registered in the DB (so the verdict read it and the
// gate cleared) but left UNTRACKED in the per-job worktree, the run branch never
// advanced, and the downstream gated job forked a run branch blind to the ledger.
func TestCompleteReviewJobAnchorsPublishedArtifactToRunBranch(t *testing.T) {
	if !haveGit(t) {
		return
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "review_anchor_551", true)

	// The adjudicator wrote its collaboration_ledger into the per-job worktree but
	// (the #551 bug) never committed it: it is untracked, the worktree HEAD has not
	// advanced, and the run branch does not contain it.
	payload := []byte("collaboration_ledger: verdict accept_with_findings (cycle 1)")
	repoPath := "docs/COLLABORATION_LEDGER_cycle_1.md"
	abs := filepath.Join(ids.worktreeRoot, filepath.FromSlash(repoPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_ledger_551", "collaboration_ledger_1", repoPath, payload, nil)

	// Precondition: the run branch does NOT yet contain the body (never committed).
	if mustGitExit(t, repoRoot, "cat-file", "-e", "refs/heads/"+ids.runBranch+":"+repoPath) == 0 {
		t.Fatalf("precondition failed: run branch %s already contains %s", ids.runBranch, repoPath)
	}

	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, false)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}

	// The review.verdict completion path must porter-commit + anchor the body.
	if err := completeReviewJob(ctx, runner, ids.repoID, job, ids.sessionID, ids.leaseID, "accept_with_findings"); err != nil {
		t.Fatalf("completeReviewJob: %v", err)
	}

	// The body is now reachable from the RUN BRANCH (committed + anchored), not just
	// registered in the DB — the downstream gated job will see the ledger.
	got := gitRun(t, repoRoot, "show", "refs/heads/"+ids.runBranch+":"+repoPath)
	if strings.TrimSpace(got) != strings.TrimSpace(string(payload)) {
		t.Fatalf("refs/heads/%s:%s = %q, want %q", ids.runBranch, repoPath, got, payload)
	}
}

func haveGit(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
		return false
	}
	return true
}
