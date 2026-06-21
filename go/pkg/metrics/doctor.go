package metrics

// RFC 0137 Phase C — doctor_problems{class} fold (deliverable #4 + F-A8).
//
// striatum_doctor_problems turns a red `doctor` into a continuously-scraped,
// pageable signal instead of a manual CLI run. Its `class` label is sourced
// EXCLUSIVELY from the STATIC `problem_records[*].check` codes the
// go/pkg/reads/doctor_*.go checks emit (e.g. recovery_sweep_cursor_wedged,
// dissent_ledger_incomplete, artifact_anchor_hash_mismatch).
//
// F-A8 (load-bearing): the always-returned `problems` strings carry DYNAMIC ids
// in their prefix (doctor.go run_needs_operator.<run_id>,
// supervisor_liveness.<supervisor_id>, recovery_sweep_cursor_latch_error.<run_id>).
// A dynamic id as a label value re-opens the A3 value-leak and A5 cardinality
// holes, so the `problems` list is NEVER consulted here — only `problem_records`,
// and only each record's static `check` field. The whole class derivation lives
// in this one file so the contract is testable at a single point
// (TestDoctorClassRejectsDynamicIdentifiers).

import "regexp"

// doctorProblemsSeriesBudget caps the distinct doctor `class` series the exporter
// will emit. The static check set is ~15 codes today, so this generous cap never
// clips in normal operation; it exists purely as the OOM backstop if the doctor
// check surface ever explodes (see budget.go).
const doctorProblemsSeriesBudget = 64

// clipFamilyDoctorProblems is the `family` label value the cardinality-clip
// counter reports when the doctor_problems budget clips. It is a stable short id,
// never a metric name or an interpolated value.
const clipFamilyDoctorProblems = "doctor_problems"

// staticCheckCodeShape is the shape every legitimate doctor check code matches:
// lowercase snake_case, no slashes, dots, colons, or hex-id runs. It is the fold
// sanitizer: any `check` value that is NOT this shape (which a static code always
// is, but an accidentally id-bearing field would not be) is bucketed to `other`
// rather than reaching the wire — a second wall behind the "never read problems"
// rule so even a misused `check` field cannot leak a dynamic id.
var staticCheckCodeShape = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// extractDoctorProblemRecords pulls the static problem records out of one doctor
// result, reading ONLY result["problem_records"] — never result["problems"]. It
// tolerates both the in-process []map[string]any shape and a decoded []any of
// maps. A nil/absent records key yields no records (and so no series).
func extractDoctorProblemRecords(result map[string]any) []map[string]any {
	if result == nil {
		return nil
	}
	raw, ok := result["problem_records"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if rec, ok := item.(map[string]any); ok {
				out = append(out, rec)
			}
		}
		return out
	}
	return nil
}

// foldDoctorProblemRecords counts problem records by their STATIC `check` code —
// the only field that may become the `class` label. A record with no usable
// string `check` is skipped; a `check` value that does not match the static-code
// shape is bucketed to `other` so it can never carry a dynamic id onto the wire.
// The caller applies the series budget to the returned map before render.
func foldDoctorProblemRecords(records []map[string]any) map[string]int {
	counts := map[string]int{}
	for _, rec := range records {
		check, _ := rec["check"].(string)
		if check == "" {
			continue
		}
		if !staticCheckCodeShape.MatchString(check) {
			check = seriesClassOther
		}
		counts[check]++
	}
	return counts
}

// foldableDoctorRecords returns the problem records one per-repo doctor run may
// contribute to the doctor_problems gauge, or nil when the run was DEGRADED: a hard
// error (derr), or a deadline-exceeded context (ctxErr — captured before the per-repo
// context is cancelled). A degraded run can carry FALSE problem records: under the
// sweep-tick doctorFoldTimeout the worktree-ref reachability probe is starved and a
// cancelled readGitAncestor reads as "unreachable" rather than "unknown". A page must
// never ride on a timed-out fold, so a degraded run contributes nothing.
func foldableDoctorRecords(result map[string]any, derr, ctxErr error) []map[string]any {
	if derr != nil || ctxErr != nil {
		return nil
	}
	return extractDoctorProblemRecords(result)
}
