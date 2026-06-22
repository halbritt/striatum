package reads

import (
	"context"
	"fmt"

	"github.com/halbritt/striatum/go/pkg/db"
)

// ownerBundleWatermarkDoctorBlock surfaces the RFC 0142 Layer 2 owner-bundle
// watermark interlock as a doctor precondition, so a pending owner bundle is
// visible BEFORE the operator restarts onto a binary that would halt on it.
//
// It reads the applied `owner_bundle_meta` watermark and compares it to the
// frontier this binary requires (db.RequiredOwnerBundleVersion). On a shortfall
// (applied >= 1 but below the required frontier) it returns a hard problem naming
// the gap and the exact out-of-band remediation command — the same condition that
// would make the daemon refuse to boot with the typed awaiting_owner_ddl halt, but
// caught at doctor time. The in-sync, tolerate-forward (applied > required), and
// fresh/single-role (applied == 0) cases are healthy and produce no problem.
//
// Daemon-global (independent of repository_id); reads no secret. Like the other
// doctor blocks it never fails closed: a read error is captured in the block so
// doctor itself still returns.
func ownerBundleWatermarkDoctorBlock(ctx context.Context, runner db.Runner) (map[string]any, []string) {
	required := db.RequiredOwnerBundleVersion
	block := map[string]any{
		"checked":  false,
		"required": required,
	}
	if runner == nil {
		block["skipped"] = "no runner"
		return block, nil
	}
	applied, err := db.OwnerBundleVersion(ctx, runner)
	if err != nil {
		block["checked"] = true
		block["error"] = err.Error()
		return block, []string{"owner_bundle_watermark.read_failed: " + err.Error()}
	}
	block["checked"] = true
	block["applied"] = applied

	switch {
	case applied == 0:
		// Fresh / single-role database with no authority schema: nothing is applied
		// out-of-band; the runtime migrations bootstrap the substrate. Not a gap.
		block["status"] = "bootstrap"
		block["note"] = "no owner bundle applied (fresh or single-role database); runtime migrations bootstrap the substrate"
		return block, nil
	case applied < required:
		// SHORTFALL: this is the awaiting_owner_ddl precondition. A restart onto a
		// binary that requires the frontier would halt cleanly; surface it now.
		block["status"] = "shortfall"
		remediation := "striatum daemon owner-ddl apply"
		block["remediation"] = remediation
		problem := fmt.Sprintf(
			"owner_bundle_watermark_shortfall: applied owner-bundle watermark %d is below the %d this binary requires; "+
				"apply the pending owner bundle(s) as the database owner (%s) BEFORE restarting onto this binary, "+
				"or the daemon will halt with awaiting_owner_ddl (RFC 0142 Layer 2)",
			applied, required, remediation)
		return block, []string{problem}
	case applied > required:
		// DOWNGRADE: a newer schema under an older binary. Policy is tolerate-forward.
		block["status"] = "ahead"
		block["note"] = "applied owner-bundle watermark is ahead of this binary's required frontier (tolerate-forward; serves)"
		return block, nil
	default:
		block["status"] = "in_sync"
		return block, nil
	}
}
