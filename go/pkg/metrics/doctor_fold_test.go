package metrics

import (
	"context"
	"errors"
	"testing"
)

// TestFoldableDoctorRecordsDropsDegradedRun locks the doctor_problems false-positive
// fix: a per-repo doctor run that errored or blew its doctorFoldTimeout must contribute
// NOTHING to the gauge, even when its (partial) result carries problem records — under
// the timeout a starved readGitAncestor reads as "unreachable" and would publish false
// worktree_head_unreachable / job_completed_without_anchor problems.
func TestFoldableDoctorRecordsDropsDegradedRun(t *testing.T) {
	result := map[string]any{
		"problem_records": []map[string]any{
			{"check": "worktree_head_unreachable"},
		},
	}

	// Healthy run: its problem records are folded.
	if got := foldableDoctorRecords(result, nil, nil); len(got) != 1 {
		t.Fatalf("healthy run: got %d records, want 1", len(got))
	}

	// Deadline-exceeded run: dropped entirely (this is the bug — a timed-out fold must
	// not page on possibly-false problems).
	if got := foldableDoctorRecords(result, nil, context.DeadlineExceeded); got != nil {
		t.Fatalf("timed-out run: got %v, want nil", got)
	}

	// Hard-errored run: likewise dropped.
	if got := foldableDoctorRecords(result, errors.New("boom"), nil); got != nil {
		t.Fatalf("errored run: got %v, want nil", got)
	}
}
