package reads

import (
	"context"
	"testing"
)

// TestReadGitAncestorCheckedDistinguishesCancellation locks the primitive behind the
// worktree-ref doctor's inconclusive handling: a cancelled context (e.g. the sweep-tick
// doctorFoldTimeout killing git) must report INCONCLUSIVE, not a confirmed negative, so
// the caller reclassifies the worktree to a warning instead of a false
// worktree_head_unreachable problem.
func TestReadGitAncestorCheckedDistinguishesCancellation(t *testing.T) {
	// Cancelled context: the git probe cannot complete → inconclusive ("unknown").
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if reachable, inconclusive := readGitAncestorChecked(cancelled, t.TempDir(), "HEAD", "refs/heads/main"); reachable || !inconclusive {
		t.Fatalf("cancelled ctx: reachable=%v inconclusive=%v, want false/true", reachable, inconclusive)
	}

	// Live context, no such ref/repo: a definite negative — never inconclusive, so a
	// genuinely-unreachable worktree still reds the doctor as before.
	if reachable, inconclusive := readGitAncestorChecked(context.Background(), t.TempDir(), "HEAD", "refs/heads/main"); reachable || inconclusive {
		t.Fatalf("live ctx negative: reachable=%v inconclusive=%v, want false/false", reachable, inconclusive)
	}
}
