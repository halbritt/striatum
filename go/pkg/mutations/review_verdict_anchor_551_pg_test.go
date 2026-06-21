package mutations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halbritt/striatum/go/pkg/pgtest"
)

// TestReviewVerdictAnchorsGitPublicationArtifact guards #551: a repo-write
// verdict-bearing job (phase_synthesis / review) writes its synthesis (e.g. a
// collaboration_ledger) into the per-job worktree, publishes it via
// artifact.publish, then completes via review.verdict. The completion path
// historically registered the artifact (verdict captured, gate cleared) but never
// committed/anchored the file — it stayed UNTRACKED in the per-job worktree, the
// run branch never advanced, and the downstream gated job (which forks from the
// run-branch tip) was blind to it.
//
// After the fix, completeReviewJob runs the SAME daemon-as-porter commit +
// run-ref anchor that work.complete runs (RFC 0125, D192). This test seeds the
// strand exactly: an untracked git_publication artifact in the worktree, then
// completes via the verdict path, and asserts the run branch advanced and now
// contains the ledger file. It FAILS before the fix (run-branch tip unchanged,
// file missing) and PASSES after.
func TestReviewVerdictAnchorsGitPublicationArtifact(t *testing.T) {
	if !haveGit(t) {
		return
	}
	ctx := context.Background()
	runner := pgtest.Pool(t).Runner
	repoRoot := t.TempDir()
	ids := seedWorktreeRequiredJob(t, ctx, runner, repoRoot, "verdict_anchor_551", true)

	// This job completes via review.verdict, not work.complete: make it a
	// repo-write verdict-bearing job. The adjudicate seat in a falsification_gate is
	// a phase_synthesis, but that job_type is owner-bundle DDL (bundle 0013) absent
	// from the default runtime-migration pgtest harness; `review` is the canonical
	// verdict-completed type in the runtime schema and reaches the IDENTICAL
	// completeReviewJob path (isVerdictCapableJobType covers both), so it exercises
	// the same strand+anchor behavior the fix repairs.
	if err := runner.Exec(ctx, `
		UPDATE striatumd.jobs SET job_type = 'review'
		 WHERE repository_id = $1 AND job_id = $2`, ids.repoID, ids.jobID); err != nil {
		t.Fatalf("set job_type=review: %v", err)
	}

	// The adjudicate lane wrote its collaboration_ledger into the per-job worktree
	// and published it, but never committed it (left untracked). Mirror the #551
	// repro: file present on disk in the worktree, declared artifact row recorded,
	// worktree HEAD un-advanced.
	repoPath := "docs/operator/artifacts/fg/dialogue/adjudicator/COLLABORATION_LEDGER_cycle_1.md"
	payload := []byte("---\nschema_version: striatum.collaboration_ledger.v1\n---\nverdict: accept_with_findings\n")
	abs := filepath.Join(ids.worktreeRoot, filepath.FromSlash(repoPath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	seedPublishedArtifact(t, ctx, runner, ids, "art_ledger_551", "collaboration_ledger", repoPath, payload, nil)

	// Precondition: the file is genuinely untracked in the worktree and absent from
	// the run-branch tip (the exact strand state #551 describes).
	if status := gitRun(t, ids.worktreeRoot, "status", "--porcelain", "--", repoPath); !strings.HasPrefix(strings.TrimSpace(status), "??") {
		t.Fatalf("precondition: artifact should be untracked in the worktree, git status = %q", status)
	}
	beforeHead := gitRun(t, repoRoot, "rev-parse", "refs/heads/"+ids.runBranch)
	if showExit := mustGitExit(t, repoRoot, "cat-file", "-e", beforeHead+":"+repoPath); showExit == 0 {
		t.Fatalf("precondition: run-branch tip should NOT yet contain %s", repoPath)
	}

	job, err := rowByID(ctx, runner, ids.repoID, "jobs", "job_id", ids.jobID, false)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}

	// Complete via the verdict-completion path (accept_with_findings clears a
	// falsification_gate). This is the exact function applyVerdict calls for an
	// accepting verdict.
	if err := completeReviewJob(ctx, runner, ids.repoID, job, ids.sessionID, ids.leaseID, "accept_with_findings"); err != nil {
		t.Fatalf("completeReviewJob: %v", err)
	}

	// The run branch must have advanced and now durably contain the ledger file —
	// the downstream commit_proposal job that forks from the tip can read it.
	afterHead := gitRun(t, repoRoot, "rev-parse", "refs/heads/"+ids.runBranch)
	if afterHead == beforeHead {
		t.Fatalf("run branch %s tip did not advance (%s) — the verdict-completed artifact was not anchored (#551)", ids.runBranch, afterHead)
	}
	// gitRun trims trailing whitespace from `git show` output, so compare the
	// trimmed bodies (the porter commits the byte-identical body; the content_sha256
	// the porter validated against the worktree HEAD is the exact-bytes check).
	got := gitRun(t, repoRoot, "show", afterHead+":"+repoPath)
	if strings.TrimSpace(got) != strings.TrimSpace(string(payload)) {
		t.Fatalf("run-branch tip %s:%s content mismatch\n got: %q\nwant: %q", afterHead, repoPath, got, payload)
	}
}
